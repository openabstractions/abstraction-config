// Package service hosts existing configuration reads for the service account.
// Callers supply run overrides; the host's environment is never their run rung.
package service

import (
	"context"
	"errors"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-identity/listen"
	"os/user"
	"strconv"
	"sync"
	"time"
)

type Host struct {
	observation observationState
	calls       chan struct{}
	listener    listen.Listener
	owner       string
	load        func(map[string]string) config.Config
	userPath    string
	userStore   casapi.Store
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	workers     sync.WaitGroup
	OnError     func(error)
	// OnStopped is called when admission stops, before active calls drain.
	// Assign it before Serve. It must return promptly.
	OnStopped func()
	// OnObservationStopped reports a failed notification source. Assign before
	// PrepareObservation or Serve; the callback runs outside observation locks.
	OnObservationStopped func()
}

func Listen(endpoint string) (*Host, error) {
	h, e := ListenWithStore(endpoint, casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}, config.UserPath())
	if e != nil {
		return nil, e
	}
	configureFileObservation(h)
	return h, nil
}

// ListenWithStore selects service-owned mutable state. The store must implement
// atomic compare/set and be safe for concurrent calls. No client selects it.
func ListenWithStore(endpoint string, store casapi.Store, userPath string) (*Host, error) {
	if store == nil || userPath == "" {
		return nil, errors.New("config service: user storage required")
	}

	owner, err := user.Current()
	if err != nil {
		return nil, err
	}
	if owner.Uid == "" {
		return nil, errors.New("config service: current principal unavailable")
	}
	listener, err := listen.Listen(endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Host{listener: listener, owner: owner.Uid, userStore: store, userPath: userPath, ctx: ctx, cancel: cancel}, nil
}
func (h *Host) Close() error {
	var err error
	h.closeOnce.Do(func() { h.cancel(); err = h.listener.Close(); h.stopObservation() })
	return err
}
func (h *Host) Serve(ctx context.Context) error {
	h.observation.mu.Lock()
	if h.observation.serving {
		h.observation.mu.Unlock()
		return errors.New("config: host already served")
	}
	h.observation.serving = true
	h.calls = make(chan struct{}, 64)
	h.observation.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { h.Close() })
	defer stop()
	defer h.workers.Wait()
	defer func() {
		if h.OnStopped != nil {
			h.OnStopped()
		}
	}()
	defer h.Close()
	for {
		connection, err := h.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || h.ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case h.calls <- struct{}{}:
		default:
			connection.Close()
			continue
		}
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			defer func() { <-h.calls }()
			defer connection.Close()
			requestContext, cancel := context.WithTimeout(h.ctx, 35*time.Second)
			defer cancel()
			call, err := listen.ReceiveFramed(requestContext, connection, listen.Program, 1<<20)
			if call != nil {
				defer call.Close()
			}
			if err == nil {
				var reply []byte
				var service string
				service, err = wire.ServiceName(call.Frame)
				receiver := &receiver{host: h, call: call}
				if err == nil {
					if service == "abstraction.config/editor@1" {
						dispatcher := wire.ConfigEditorDispatcher{Handler: receiver}
						reply, err = dispatcher.ExchangeFrame(call.Frame)
					} else if service == "abstraction.config/observer@1" {
						dispatcher := wire.ConfigObserverDispatcher{Handler: receiver}
						reply, err = dispatcher.ExchangeFrame(call.Frame)
					} else {
						dispatcher := wire.ConfigReaderDispatcher{Handler: receiver}
						reply, err = dispatcher.ExchangeFrame(call.Frame)
					}
				}
				if err == nil {
					err = call.Reply(reply)
				}
			}
			if err != nil && h.OnError != nil && h.ctx.Err() == nil {
				h.OnError(err)
			}
		}()
	}
}

type receiver struct {
	host *Host
	call *listen.FramedCall
}

func (r *receiver) Read(overrides wire.RunOverrides) (wire.Snapshot, error) {
	if err := r.authorize(); err != nil {
		return wire.Snapshot{}, err
	}
	overridesMap := map[string]string{"ABSTRACTION_NAS_STORE": overrides.NasStore, "ABSTRACTION_STORE": overrides.Store, "ABSTRACTION_LOG": overrides.LogSink, "ABSTRACTION_LOG_SERVICE": overrides.LogService}
	var c config.Config
	if r.host.load != nil {
		c = r.host.load(overridesMap)
	} else {
		var err error
		c, err = config.LoadWithUserStore(r.host.userStore, r.host.userPath, overridesMap)
		if err != nil {
			return wire.Snapshot{}, &wire.ServiceError{Code: "storage_unavailable", Message: "user configuration could not be read"}
		}
	}
	origin := func(key string) wire.Origin { o := c.Origin(key); return wire.Origin{Rung: o.Rung, Path: o.Path} }
	off := c.Off
	if off == nil {
		off = map[string]string{}
	}
	return wire.Snapshot{NasStore: c.NASStore, Store: c.Store, LogSink: c.LogSink, LogService: c.LogService, Off: off, Stamp: c.Stamp(), Origins: wire.Origins{NasStore: origin("nas_store"), Store: origin("store"), LogSink: origin("log_sink"), LogService: origin("log_service"), Off: origin("off")}}, nil
}

func (r *receiver) authorize() error {
	p, err := r.call.Peer()
	if err != nil {
		return &wire.ServiceError{Code: "caller_unavailable", Message: "caller identity could not be rechecked"}
	}
	u, err := p.User.AtLeast(listen.Program.User)
	if err != nil {
		return &wire.ServiceError{Code: "identity_required", Message: "kernel user identity required"}
	}
	principal := ""
	if u.Kind == "windows" {
		principal = u.SID
	} else if u.Kind == "posix" {
		principal = strconv.Itoa(u.UID)
	}
	if principal == "" || principal != r.host.owner {
		return &wire.ServiceError{Code: "wrong_user", Message: "configuration service belongs to another user"}
	}

	return nil
}
