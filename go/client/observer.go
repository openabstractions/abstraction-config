package client

import (
	"context"
	"errors"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-identity/listen"
	"time"
	"unicode/utf8"
)

type ConfigObservation = wire.ConfigObservation

// Observer is a reusable binding to the latest-snapshot observation service.
type Observer struct {
	transport listen.FrameClient
	initErr   error
}

func NewObserver(endpoint string) *Observer {
	c := NewObserverWithTransport(listen.FrameClient{Endpoint: endpoint})
	if endpoint == "" {
		c.initErr = emptyEndpointError()
	}
	return c
}

// NewObserverWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewObserverWithTransport(transport listen.FrameClient) *Observer {
	c := &Observer{transport: transport.WithDefaults(DefaultTimeout, 1<<20)}
	if transport.Endpoint == "" && transport.Dialer == nil {
		c.initErr = emptyEndpointError()
	}
	return c
}
func (c *Observer) WithTimeout(timeout time.Duration) (*Observer, error) {
	if timeout <= 0 || timeout > 35*time.Second {
		return nil, errors.New("config: timeout must be positive and at most 35 seconds")
	}
	copy := *c
	copy.transport.Timeout = timeout
	return &copy, nil
}

// ObserveContext uses explicit run overrides; changed overrides require an
// empty cursor. Canceling this wait does not cancel a setting mutation.
func (c *Observer) ObserveContext(ctx context.Context, overrides RunOverrides, cursor string, waitMS int64) (ConfigObservation, error) {
	var result ConfigObservation
	var err error
	c.ObservePagesContext(ctx, overrides, cursor, waitMS)(func(page ConfigObservation, e error) bool {
		result, err = page, e
		return false
	})
	return result, err
}

// ObservePagesContext yields bounded observations using the generated receiver.
// Each invocation resumes at its last validated cursor. Errors end an invocation;
// callers own reconnects and resetting to an empty cursor after a gap. Returning
// false stops before another request. Cancellation closes the active wait only.
func (c *Observer) ObservePagesContext(ctx context.Context, overrides RunOverrides, cursor string, waitMS int64) func(func(ConfigObservation, error) bool) {
	return func(yield func(ConfigObservation, error) bool) {
		if e := c.observationBounds(ctx, overrides, cursor, waitMS); e != nil {
			yield(ConfigObservation{}, e)
			return
		}
		wire.NewConfigObserverClient(observeTransport(c.transport, waitMS).WithContext(ctx)).ObservePages(overrides, cursor, waitMS)(func(result ConfigObservation, e error) bool {
			if e != nil {
				yield(result, e)
				return false
			}
			if e = validateObservation(result, cursor); e != nil {
				yield(ConfigObservation{}, e)
				return false
			}
			cursor = result.Cursor
			return yield(result, nil)
		})
	}
}

func (c *Observer) observationBounds(ctx context.Context, overrides RunOverrides, cursor string, waitMS int64) error {
	if c.initErr != nil {
		return c.initErr
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if waitMS < 0 || waitMS > 30000 || len(cursor) > 512 || !utf8.ValidString(cursor) {
		return errors.New("config: invalid observation bounds")
	}
	for _, v := range []string{overrides.NasStore, overrides.Store, overrides.LogSink, overrides.LogService} {
		if len(v) > 4096 {
			return errors.New("config: oversized override")
		}
	}
	return nil
}

func validateObservation(result ConfigObservation, cursor string) error {
	if result.Outcome == wire.ConfigObservationOutcomeSnapshot {
		if result.Cursor == "" || result.Cursor == cursor || len(result.Cursor) > 512 {
			return errors.New("config: malformed observation snapshot")
		}
	} else if result.Cursor != cursor || (result.Outcome == wire.ConfigObservationOutcomeUnchanged && cursor == "") {
		return errors.New("config: observation refusal changed cursor")
	}
	return nil
}
