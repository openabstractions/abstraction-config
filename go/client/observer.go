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
type Observer struct{ transport listen.FrameClient }

func NewObserver(endpoint string) *Observer {
	return NewObserverWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewObserverWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewObserverWithTransport(transport listen.FrameClient) *Observer {
	return &Observer{transport.WithDefaults(DefaultTimeout, 1<<20)}
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
	if e := ctx.Err(); e != nil {
		return ConfigObservation{}, e
	}
	if waitMS < 0 || waitMS > 30000 || len(cursor) > 512 || !utf8.ValidString(cursor) {
		return ConfigObservation{}, errors.New("config: invalid observation bounds")
	}
	for _, v := range []string{overrides.NasStore, overrides.Store, overrides.LogSink, overrides.LogService} {
		if len(v) > 4096 {
			return ConfigObservation{}, errors.New("config: oversized override")
		}
	}
	result, e := wire.NewConfigObserverClient(c.transport.WithContext(ctx)).Observe(overrides, cursor, waitMS)
	if e != nil {
		return result, e
	}
	if result.Outcome == wire.ConfigObservationOutcomeSnapshot {
		if result.Snapshot == nil || result.Cursor == "" || result.Cursor == cursor || len(result.Cursor) > 512 {
			return ConfigObservation{}, errors.New("config: malformed observation snapshot")
		}
	} else if result.Snapshot != nil || result.Cursor != cursor || (result.Outcome == wire.ConfigObservationOutcomeUnchanged && cursor == "") {
		return ConfigObservation{}, errors.New("config: observation refusal changed cursor")
	}
	return result, nil
}
