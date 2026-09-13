package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
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
	changed                      chan struct{}
	slots                        chan struct{}
	stop                         func()
	done                         chan struct{}
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
func configureFileObservation(h *Host) {
	_ = h.EnableObservation(func(ctx context.Context) (<-chan struct{}, func(), error) {
		subscription, e := config.WatchInvalidations()
		if e != nil {
			return nil, nil, e
		}
		return subscription.Changes(), func() { subscription.Close() }, nil
	})
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
	o.changed = make(chan struct{})
	o.slots = make(chan struct{}, 32)
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
	go func() {
		defer close(o.done)
		for {
			select {
			case <-h.ctx.Done():
				return
			case _, ok := <-changes:
				o.mu.Lock()
				close(o.changed)
				o.changed = make(chan struct{})
				if !ok {
					o.failed = true
				}
				o.mu.Unlock()
				if !ok {
					if h.OnObservationStopped != nil {
						h.OnObservationStopped()
					}
					return
				}
			}
		}
	}()
	return true
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
	refusal := func(outcome string) (wire.ConfigObservation, error) {
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
	waitCtx := r.call.WaitContext()
	timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
	defer timer.Stop()
	expired := waitMS == 0
	held := false
	defer func() {
		if held {
			<-o.slots
		}
	}()
	for {
		if e := waitCtx.Err(); e != nil {
			return wire.ConfigObservation{}, e
		}
		o.mu.Lock()
		changed, failed := o.changed, o.failed
		o.mu.Unlock()
		if failed {
			return refusal(wire.ConfigObservationOutcomeUnavailable)
		}
		snapshot, e := r.Read(overrides)
		if e != nil {
			return wire.ConfigObservation{}, e
		}
		sum := sha256.Sum256(wire.Encode(&snapshot))
		next := o.epoch + ":" + binding + ":" + hex.EncodeToString(sum[:])
		if e = r.authorize(); e != nil {
			return wire.ConfigObservation{}, e
		}
		if next != cursor {
			return wire.ConfigObservation{Outcome: wire.ConfigObservationOutcomeSnapshot, Cursor: next, Snapshot: &snapshot}, nil
		}
		if expired {
			return refusal(wire.ConfigObservationOutcomeUnchanged)
		}
		if !held {
			select {
			case o.slots <- struct{}{}:
				held = true
			default:
				return refusal(wire.ConfigObservationOutcomeUnavailable)
			}
		}
		select {
		case <-waitCtx.Done():
			return wire.ConfigObservation{}, waitCtx.Err()
		case <-timer.C:
			expired = true
		case <-changed:
		}
	}
}
