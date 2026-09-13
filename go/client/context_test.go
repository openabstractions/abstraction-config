package client

import (
	"context"
	"errors"
	"fmt"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestContextMethodsRefuseExpiredWait(t *testing.T) {
	c := New("invalid endpoint must not be opened")
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		want := error(context.Canceled)
		if expired {
			cancel()
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			want = context.DeadlineExceeded
		} else {
			cancel()
		}
		defer cancel()
		t.Run("method0", func(t *testing.T) {
			_, err := c.ReadContext(ctx)
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
		})
		t.Run("method1", func(t *testing.T) {
			_, err := c.ReadWithOverridesContext(ctx, RunOverrides{})
			if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
		})
	}
}

type contextProvider struct{}

func (contextProvider) Read(wire.RunOverrides) (wire.Snapshot, error) { return wire.Snapshot{}, nil }
func TestContextCancellationDuringCallAndDefaultReuse(t *testing.T) {
	dir, err := os.MkdirTemp("", "oa-ctx-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	endpoint := filepath.Join(dir, "service.sock")
	if runtime.GOOS == "windows" {
		endpoint = fmt.Sprintf(`\\.\pipe\oa-client-context-%d-%d`, os.Getpid(), time.Now().UnixNano())
	}
	l, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	served := make(chan error, 1)
	go func() {
		d := wire.ConfigReaderDispatcher{Handler: contextProvider{}}
		for i := 0; i < 2; i++ {
			conn, err := l.Accept()
			if err != nil {
				served <- err
				return
			}
			wait, finish := context.WithTimeout(context.Background(), 3*time.Second)
			call, err := listen.ReceiveFramed(wait, conn, identity.Need{User: identity.ProofKernel, Process: identity.ProofPID, Path: identity.ProofPID}, 0)
			if err != nil {
				finish()
				served <- err
				return
			}
			if i == 0 {
				close(entered)
				<-release
			} else {
				var reply []byte
				reply, err = d.ExchangeFrame(call.Frame)
				if err == nil {
					err = call.Reply(reply)
				}
			}
			call.Close()
			finish()
			if err != nil {
				served <- err
				return
			}
		}
		served <- nil
	}()
	c := New(endpoint)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := c.ReadWithOverridesContext(ctx, RunOverrides{}); result <- err }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not receive")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("call ignored cancellation")
	}
	release <- struct{}{}
	if _, err := c.ReadWithOverrides(RunOverrides{}); err != nil {
		t.Fatalf("default reuse: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}
