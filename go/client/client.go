// Package client reads configuration through the installed per-user service.
// It does not import a provider, read configuration files, or start a fallback.
package client

import (
	"context"
	"fmt"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"time"
)

const EnvEndpoint = "ABSTRACTION_CONFIG_ENDPOINT"

// DefaultTimeout is how long a reader, editor or observer waits for one call
// when its transport names no timeout and the call's context no deadline. A
// service that consults a policy, such as the runtime's rights decision on
// ReplaceUser, answers within it. A transport timeout or context deadline
// replaces it.
const DefaultTimeout = 2 * time.Second

// ObserveMargin is the time an observation call has beyond its requested wait
// to connect and carry the reply. An observation's call limit is its wait
// plus this margin whenever the transport's own limit is shorter (CFG-W12),
// so a long wait is never cut off by the two-second default.
const ObserveMargin = 5 * time.Second

// observeTransport is transport with a call limit that covers an
// observation's wait of waitMS milliseconds.
func observeTransport(transport listen.FrameClient, waitMS int64) listen.FrameClient {
	if limit := time.Duration(waitMS)*time.Millisecond + ObserveMargin; waitMS > 0 && transport.Timeout < limit {
		transport.Timeout = limit
	}
	return transport
}

type Snapshot = wire.Snapshot
type RunOverrides = wire.RunOverrides
type Client struct {
	transport listen.FrameClient
	initErr   error
}

// DefaultEndpoint returns empty when the account namespace cannot be resolved.
func DefaultEndpoint() string { endpoint, _ := checkedDefaultEndpoint(); return endpoint }
func checkedDefaultEndpoint() (string, error) {
	if endpoint := os.Getenv(EnvEndpoint); endpoint != "" {
		return endpoint, nil
	}
	return bootstrap.InstalledEndpoint("config-v1")
}
func emptyEndpointError() error {
	_, err := checkedDefaultEndpoint()
	if err != nil {
		return err
	}
	return fmt.Errorf("config: empty endpoint")
}

func Discover() *Client {
	endpoint, err := checkedDefaultEndpoint()
	c := New(endpoint)
	c.initErr = err
	return c
}
func New(endpoint string) *Client {
	c := NewWithTransport(listen.FrameClient{Endpoint: endpoint})
	if endpoint == "" {
		c.initErr = emptyEndpointError()
	}
	return c
}

// NewWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewWithTransport(transport listen.FrameClient) *Client {
	c := &Client{transport: transport.WithDefaults(DefaultTimeout, 1<<20)}
	if transport.Endpoint == "" && transport.Dialer == nil {
		c.initErr = emptyEndpointError()
	}
	return c
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
	if c.initErr != nil {
		return Snapshot{}, c.initErr
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return wire.NewConfigReaderClient(c.transport.WithContext(ctx)).Read(overrides)
}
