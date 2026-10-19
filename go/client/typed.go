package client

import (
	"context"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

// ReadTypedContext returns service-effective entries. Explicit process changes
// use the generated OverlayConfigProcess helper after this call.
func (c *Client) ReadTypedContext(ctx context.Context, names []string) (wire.ReadResult, error) {
	if c.initErr != nil {
		return wire.ReadResult{}, c.initErr
	}
	if err := ctx.Err(); err != nil {
		return wire.ReadResult{}, err
	}
	return wire.NewConfigReaderV2Client(c.transport.WithContext(ctx)).Read(names)
}

func (c *Client) ObserveTypedContext(ctx context.Context, cursor string, waitMS int64) (wire.ConfigObservationV2, error) {
	if c.initErr != nil {
		return wire.ConfigObservationV2{}, c.initErr
	}
	if err := ctx.Err(); err != nil {
		return wire.ConfigObservationV2{}, err
	}
	return wire.NewConfigObserverV2Client(observeTransport(c.transport, waitMS).WithContext(ctx)).Observe(cursor, waitMS)
}
