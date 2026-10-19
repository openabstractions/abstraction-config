package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	watch "github.com/openabstractions/abstraction-watch/go"
)

// ObservationSource supplies invalidations for this host's selected provider,
// including external writes. It must return promptly, honor ctx, and supply an
// idempotent stop which joins its resources. Closing changes reports failure.
type ObservationSource func(context.Context) (changes <-chan struct{}, stop func(), err error)
type observationState struct {
	mu                           sync.Mutex
	serving, initialized, failed bool
	source                       ObservationSource
	epoch                        string
	gate                         *watch.Gate
	stop                         func()
	done                         chan struct{}
	typedGood                    map[string]typedGoodSnapshot
}

// EnableObservation selects a matching trusted provider notification source.
// Configure before Serve. Injected stores have no implicit file notification.
func (h *Host) EnableObservation(source ObservationSource) error {
	h.observation.mu.Lock()
	defer h.observation.mu.Unlock()
	if h.observation.serving || h.observation.initialized {
		return errors.New("config: observation already serving")
	}
	h.observation.source = source
	return nil
}

// ObservationAvailable reports configured support and known source failure.
// Before first use this is configuration metadata, not a live readiness probe.
func (h *Host) ObservationAvailable() bool {
	h.observation.mu.Lock()
	defer h.observation.mu.Unlock()
	return h.ctx.Err() == nil && h.observation.source != nil && !h.observation.failed
}

// The service creates its user location's directory before observing, so an
// empty profile is observable at once (CFG-W6).
func configureFileObservation(h *Host) {
	path := h.userPath
	_ = h.EnableObservation(func(ctx context.Context) (<-chan struct{}, func(), error) {
		if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
			return nil, nil, e
		}
		return subscribed(config.WatchInvalidations())
	})
}

// FileObservation is the source for a host whose selected store is one file
// at path, such as a runtime's state-directory store. It creates the file's
// directory and reports native notification on it.
func FileObservation(path string) ObservationSource {
	return func(ctx context.Context) (<-chan struct{}, func(), error) {
		if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
			return nil, nil, e
		}
		return subscribed(config.WatchFile(path))
	}
}

func subscribed(subscription *config.InvalidationSubscription, e error) (<-chan struct{}, func(), error) {
	if e != nil {
		return nil, nil, e
	}
	return subscription.Changes(), func() { subscription.Close() }, nil
}

// PrepareObservation establishes the configured notification source before a
// runtime advertises readiness. The host owns its cleanup through Close.
func (h *Host) PrepareObservation() bool { return h.startObservation() }

func (h *Host) startObservation() bool {
	o := &h.observation
	o.mu.Lock()
	defer o.mu.Unlock()
	// Close cancels before acquiring this mutex. A source already being started
	// publishes its cleanup under the same lock before Close can inspect it.
	if h.ctx.Err() != nil {
		return false
	}
	if o.source == nil {
		return false
	}
	if o.initialized {
		return !o.failed
	}
	o.initialized = true
	var epoch [16]byte
	if _, e := rand.Read(epoch[:]); e != nil {
		o.failed = true
		return false
	}
	o.epoch = hex.EncodeToString(epoch[:])
	o.gate = watch.NewGate(watch.DefaultWaiters)
	changes, stop, e := o.source(h.ctx)
	if e != nil || changes == nil || stop == nil {
		if stop != nil {
			stop()
		}
		o.failed = true
		return false
	}
	o.stop = stop
	o.done = make(chan struct{})
	var restartChanges <-chan struct{}
	if h.typedOptions.RestartState != nil {
		_, restartChanges = h.typedOptions.RestartState.snapshot()
	}
	go func() {
		defer close(o.done)
		armed, delay := time.Now(), rearmFirstDelay
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-restartChanges:
				_, restartChanges = h.typedOptions.RestartState.snapshot()
				o.gate.Advance()
			case _, ok := <-changes:
				if ok {
					o.gate.Advance()
					continue
				}
				// A source that ends again soon after rearming waits longer.
				if time.Since(armed) > rearmMaxDelay {
					delay = rearmFirstDelay
				}
				if changes, delay = h.rearmObservation(delay); changes == nil {
					return
				}
				armed = time.Now()
			}
		}
	}()
	return true
}

// Rearm delays between attempts to recreate the watched folder.
const (
	rearmFirstDelay = 50 * time.Millisecond
	rearmMaxDelay   = 5 * time.Second
)

// rearmObservation runs when the source ends, typically because the watched
// configuration folder was deleted or moved. It stops the ended source, lets
// waiters reread, and starts the source again; the source recreates the folder.
// Each attempt waits delay first, which doubles up to rearmMaxDelay; on Windows
// a deleted folder stays pending until its last handle closes. Until a source
// starts the observation reports unavailable. It returns the new changes and
// the next delay, or nil changes once the host closes.
func (h *Host) rearmObservation(delay time.Duration) (<-chan struct{}, time.Duration) {
	o := &h.observation
	o.mu.Lock()
	ended := o.stop
	o.mu.Unlock()
	ended()
	if h.ctx.Err() != nil {
		return nil, delay
	}
	o.gate.Advance()
	h.observationLog(errors.New("config: the watched configuration folder went away; recreating it and watching again"))
	reported := false
	for {
		select {
		case <-h.ctx.Done():
			return nil, delay
		case <-time.After(delay):
		}
		delay = min(2*delay, rearmMaxDelay)
		changes, stop, e := o.source(h.ctx)
		if e == nil && changes != nil && stop != nil {
			o.mu.Lock()
			if h.ctx.Err() != nil {
				o.mu.Unlock()
				stop()
				return nil, delay
			}
			o.stop = stop
			wasFailed := o.failed
			o.failed = false
			o.mu.Unlock()
			// Changes made between the end and the new watch are read now.
			o.gate.Advance()
			h.observationLog(errors.New("config: watching the configuration folder again"))
			if wasFailed && h.OnObservationStopped != nil {
				h.OnObservationStopped()
			}
			return changes, delay
		}
		if stop != nil {
			stop()
		}
		if !reported {
			reported = true
			o.mu.Lock()
			o.failed = true
			o.mu.Unlock()
			if e == nil {
				e = errors.New("source returned no changes")
			}
			h.observationLog(fmt.Errorf("config: cannot watch the configuration folder yet, retrying: %w", e))
			if h.OnObservationStopped != nil {
				h.OnObservationStopped()
			}
		}
	}
}

func (h *Host) observationLog(e error) {
	if h.OnError != nil && h.ctx.Err() == nil {
		h.OnError(e)
	}
}
func (h *Host) stopObservation() {
	o := &h.observation
	o.mu.Lock()
	stop, done := o.stop, o.done
	o.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
}
func observationRequest(overrides wire.RunOverrides, cursor string, waitMS int64) bool {
	if waitMS < 0 || waitMS > 30000 || len(cursor) > 512 || !utf8.ValidString(cursor) {
		return false
	}
	for _, v := range []string{overrides.NasStore, overrides.Store, overrides.LogSink, overrides.LogService} {
		if len(v) > 4096 || !utf8.ValidString(v) {
			return false
		}
	}
	return true
}
func observationBinding(overrides wire.RunOverrides) string {
	raw, _ := json.Marshal(overrides)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (r *receiver) Observe(overrides wire.RunOverrides, cursor string, waitMS int64) (wire.ConfigObservation, error) {
	refusal := func(outcome wire.ConfigObservationOutcome) (wire.ConfigObservation, error) {
		return wire.ConfigObservation{Outcome: outcome, Cursor: cursor}, nil
	}
	if e := r.authorize(); e != nil {
		return wire.ConfigObservation{}, e
	}
	if !observationRequest(overrides, cursor, waitMS) {
		return refusal(wire.ConfigObservationOutcomeInvalid)
	}
	o := &r.host.observation
	o.mu.Lock()
	configured := o.source != nil
	o.mu.Unlock()
	if !configured {
		return refusal(wire.ConfigObservationOutcomeUnsupported)
	}
	if !r.host.startObservation() {
		return refusal(wire.ConfigObservationOutcomeUnavailable)
	}
	binding := observationBinding(overrides)
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 3 || len(parts[0]) != 32 || len(parts[1]) != 64 || len(parts[2]) != 64 {
			return refusal(wire.ConfigObservationOutcomeInvalid)
		}
		for _, part := range parts {
			if _, e := hex.DecodeString(part); e != nil {
				return refusal(wire.ConfigObservationOutcomeInvalid)
			}
		}
		if parts[0] != o.epoch || parts[1] != binding {
			return refusal(wire.ConfigObservationOutcomeGap)
		}
	}
	var answer wire.ConfigObservation
	outcome, e := o.gate.Wait(r.call.WaitContext(), time.Duration(waitMS)*time.Millisecond, watch.Probe{
		Read: func() (bool, error) {
			snapshot, e := r.Read(overrides)
			if e != nil {
				return false, e
			}
			sum := sha256.Sum256(wire.Encode(&snapshot))
			next := o.epoch + ":" + binding + ":" + hex.EncodeToString(sum[:])
			answer = wire.ConfigObservation{Outcome: wire.ConfigObservationOutcomeSnapshot, Cursor: next, Snapshot: &snapshot}
			return next != cursor, nil
		},
		Recheck: r.authorize,
	})
	if e != nil {
		return wire.ConfigObservation{}, e
	}
	switch outcome {
	case watch.Answered:
		return answer, nil
	case watch.Unchanged:
		return refusal(wire.ConfigObservationOutcomeUnchanged)
	}
	return refusal(wire.ConfigObservationOutcomeUnavailable)
}
