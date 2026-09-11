// Package client reads configuration through the installed per-user service.
// It does not import a provider, read configuration files, or start a fallback.
package client

import (
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
	return &Client{transport: listen.FrameClient{Endpoint: endpoint, Timeout: 2 * time.Second, MaxFrame: 1 << 20}}
}

// Read captures this caller's existing run overrides at call time. Empty values
// leave file answers intact. Overrides are configuration claims, not identity.
func (c *Client) Read() (Snapshot, error) {
	return c.ReadWithOverrides(RunOverrides{NasStore: os.Getenv("ABSTRACTION_NAS_STORE"), Store: os.Getenv("ABSTRACTION_STORE"), LogSink: os.Getenv("ABSTRACTION_LOG"), LogService: os.Getenv("ABSTRACTION_LOG_SERVICE")})
}
func (c *Client) ReadWithOverrides(overrides RunOverrides) (Snapshot, error) {
	return wire.NewConfigReaderClient(&c.transport).Read(overrides)
}
