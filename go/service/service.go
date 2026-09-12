// Package service hosts existing configuration reads for the service account.
// Callers supply run overrides; the host's environment is never their run rung.
package service

import (
	"context"
	"errors"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-identity/listen"
	"os/user"
	"strconv"
	"sync"
	"time"
)

type Host struct {
	listener  listen.Listener
	owner     string
	load      func(map[string]string) config.Config
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	workers   sync.WaitGroup
	OnError   func(error)
	// OnStopped is called when admission stops, before active calls drain.
	// Assign it before Serve. It must return promptly.
	OnStopped func()
}

func Listen(endpoint string) (*Host, error) {
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
	return &Host{listener: listener, owner: owner.Uid, load: config.LoadWithOverrides, ctx: ctx, cancel: cancel}, nil
}
func (h *Host) Close() error {
	var err error
	h.closeOnce.Do(func() { h.cancel(); err = h.listener.Close() })
	return err
}
func (h *Host) Serve(ctx context.Context) error {
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
		h.workers.Add(1)
		go func() {
			defer h.workers.Done()
			defer connection.Close()
			requestContext, cancel := context.WithTimeout(h.ctx, 5*time.Second)
			defer cancel()
			call, err := listen.ReceiveFramed(requestContext, connection, listen.Program, 1<<20)
			if call != nil {
				defer call.Close()
			}
			if err == nil {
				dispatcher := wire.ConfigReaderDispatcher{Handler: &receiver{host: h, call: call}}
				var reply []byte
				reply, err = dispatcher.ExchangeFrame(call.Frame)
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
	p, err := r.call.Peer()
	if err != nil {
		return wire.Snapshot{}, &wire.ServiceError{Code: "caller_unavailable", Message: "caller identity could not be rechecked"}
	}
	u, err := p.User.AtLeast(listen.Program.User)
	if err != nil {
		return wire.Snapshot{}, &wire.ServiceError{Code: "identity_required", Message: "kernel user identity required"}
	}
	principal := ""
	if u.Kind == "windows" {
		principal = u.SID
	} else if u.Kind == "posix" {
		principal = strconv.Itoa(u.UID)
	}
	if principal == "" || principal != r.host.owner {
		return wire.Snapshot{}, &wire.ServiceError{Code: "wrong_user", Message: "configuration service belongs to another user"}
	}
	c := r.host.load(map[string]string{"ABSTRACTION_NAS_STORE": overrides.NasStore, "ABSTRACTION_STORE": overrides.Store, "ABSTRACTION_LOG": overrides.LogSink, "ABSTRACTION_LOG_SERVICE": overrides.LogService})
	origin := func(key string) wire.Origin { o := c.Origin(key); return wire.Origin{Rung: o.Rung, Path: o.Path} }
	off := c.Off
	if off == nil {
		off = map[string]string{}
	}
	return wire.Snapshot{NasStore: c.NASStore, Store: c.Store, LogSink: c.LogSink, LogService: c.LogService, Off: off, Stamp: c.Stamp(), Origins: wire.Origins{NasStore: origin("nas_store"), Store: origin("store"), LogSink: origin("log_sink"), LogService: origin("log_service"), Off: origin("off")}}, nil
}
