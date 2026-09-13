package client

import (
	"context"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-identity/listen"
	"time"
)

type UserSettings = wire.UserSettings
type UserSnapshot = wire.UserSnapshot
type UserReplaceResult = wire.UserReplaceResult

// Editor edits one service-owned user rung. It retains no caller context and
// supports concurrent calls. A waiting error leaves replacement unresolved.
type Editor struct{ transport listen.FrameClient }

func NewEditor(endpoint string) *Editor {
	return NewEditorWithTransport(listen.FrameClient{Endpoint: endpoint})
}

// NewEditorWithTransport retains the caller's endpoint, server trust and waiting limits.
func NewEditorWithTransport(transport listen.FrameClient) *Editor {
	return &Editor{transport: transport.WithDefaults(2*time.Second, 1<<20)}
}
func (c *Editor) ReadUser() (wire.UserSnapshot, error) {
	return c.ReadUserContext(context.Background())
}
func (c *Editor) ReadUserContext(ctx context.Context) (wire.UserSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return wire.UserSnapshot{}, err
	}
	return wire.NewConfigEditorClient(c.transport.WithContext(ctx)).ReadUser()
}
func (c *Editor) ReplaceUser(expected string, values wire.UserSettings) (wire.UserReplaceResult, error) {
	return c.ReplaceUserContext(context.Background(), expected, values)
}
func (c *Editor) ReplaceUserContext(ctx context.Context, expected string, values wire.UserSettings) (wire.UserReplaceResult, error) {
	if err := ctx.Err(); err != nil {
		return wire.UserReplaceResult{}, err
	}
	return wire.NewConfigEditorClient(c.transport.WithContext(ctx)).ReplaceUser(expected, values)
}
