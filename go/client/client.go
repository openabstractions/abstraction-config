// Package client reads configuration through the installed per-user service.
// It does not import a provider, read configuration files, or start a fallback.
package client

import (
	"context"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"time"
)

const EnvEndpoint = "ABSTRACTION_CONFIG_ENDPOINT"

type Snapshot = wire.Snapshot
type RunOverrides = wire.RunOverrides
type Client struct{ transport listen.FrameClient }

func DefaultEndpoint() string {
	if value := os.Getenv(EnvEndpoint); value != "" {
		return value
	}
	return listen.Endpoint("config-v1")
}
func Discover() *Client { return New(DefaultEndpoint()) }
func New(endpoint string) *Client {
	return NewWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewWithTransport(transport listen.FrameClient) *Client {
	return &Client{transport: transport.WithDefaults(2*time.Second, 1<<20)}
}

// Read captures this caller's existing run overrides at call time. Empty values
// leave file answers intact. Overrides are configuration claims, not identity.
func (c *Client) Read() (Snapshot, error) {
	return c.ReadContext(context.Background())
}

// ReadContext captures run overrides and bounds this call by ctx.
func (c *Client) ReadContext(ctx context.Context) (Snapshot, error) {
	return c.ReadWithOverridesContext(ctx, RunOverrides{NasStore: os.Getenv("ABSTRACTION_NAS_STORE"), Store: os.Getenv("ABSTRACTION_STORE"), LogSink: os.Getenv("ABSTRACTION_LOG"), LogService: os.Getenv("ABSTRACTION_LOG_SERVICE")})
}
func (c *Client) ReadWithOverrides(overrides RunOverrides) (Snapshot, error) {
	return c.ReadWithOverridesContext(context.Background(), overrides)
}

// ReadWithOverridesContext uses ctx only for this operation.
func (c *Client) ReadWithOverridesContext(ctx context.Context, overrides RunOverrides) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return wire.NewConfigReaderClient(c.transport.WithContext(ctx)).Read(overrides)
}
