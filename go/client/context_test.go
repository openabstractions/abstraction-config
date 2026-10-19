package client

import (
	"context"
	"errors"
	"fmt"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-config/go/internal/personfolders"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/go/privatedir/privatetest"
	"github.com/openabstractions/abstraction-identity/listen"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
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

// TestMain confines per-user folders to the canonical owned test fixture.
func TestMain(m *testing.M) { os.Exit(personfolders.Main(m)) }

type pagesProvider struct {
	observe func(wire.RunOverrides, string, int64) (wire.ConfigObservation, error)
}

var observerPageSerial atomic.Uint64

func (p pagesProvider) Observe(o wire.RunOverrides, cursor string, wait int64) (wire.ConfigObservation, error) {
	return p.observe(o, cursor, wait)
}

// Use the same authenticated framed receiver as the context-call fixture above.
// A malformed successful wire reply must never become the resume cursor.
func TestObserverPagesValidateResumeGapAndBreak(t *testing.T) {
	endpoint := listen.Endpoint(fmt.Sprintf("config-client-pages-%d-%d", os.Getpid(), observerPageSerial.Add(1)))
	if runtime.GOOS != "windows" {
		// macOS's per-user TMPDIR makes the socket path too long for sun_path.
		endpoint = listen.Endpoint(filepath.Join(privatetest.SocketDir(t), "pages.sock"))
	}
	l, err := listen.Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	wantCursor := []string{"", "good", "good", ""}
	pages := []wire.ConfigObservation{
		{Outcome: wire.ConfigObservationOutcomeSnapshot, Cursor: "good", Snapshot: &wire.Snapshot{}},
		{Outcome: wire.ConfigObservationOutcomeSnapshot, Cursor: "poison"},
		{Outcome: wire.ConfigObservationOutcomeGap, Cursor: "good"},
		{Outcome: wire.ConfigObservationOutcomeSnapshot, Cursor: "fresh", Snapshot: &wire.Snapshot{}},
	}
	served := make(chan error, 1)
	go func() {
		for i := range pages {
			conn, e := l.Accept()
			if e != nil {
				served <- e
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			call, e := listen.ReceiveFramed(ctx, conn, identity.Need{User: identity.ProofKernel, Process: identity.ProofPID, Path: identity.ProofPID}, 0)
			if e == nil {
				d := wire.ConfigObserverDispatcher{Handler: pagesProvider{observe: func(_ wire.RunOverrides, cursor string, wait int64) (wire.ConfigObservation, error) {
					if cursor != wantCursor[i] || wait != 0 {
						t.Errorf("request %d cursor=%q wait=%d", i, cursor, wait)
					}
					return pages[i], nil
				}}}
				var reply []byte
				reply, e = d.ExchangeFrame(call.Frame)
				if e == nil {
					e = call.Reply(reply)
				}
				call.Close()
			} else {
				conn.Close()
			}
			cancel()
			if e != nil {
				served <- e
				return
			}
		}
		served <- nil
	}()
	c := NewObserver(endpoint)
	seq := c.ObservePagesContext(context.Background(), RunOverrides{}, "", 0)
	count := 0
	seq(func(page ConfigObservation, e error) bool {
		count++
		if count == 1 {
			if e != nil || page.Cursor != "good" {
				t.Fatal(page, e)
			}
			return true
		}
		if e == nil || page.Cursor != "" {
			t.Fatal("malformed page escaped", page, e)
		}
		return true // Validation failure ends even when the caller requests more.
	})
	if count != 2 {
		t.Fatal("unexpected yield count", count)
	}
	seq(func(page ConfigObservation, e error) bool {
		if e != nil || page.Outcome != wire.ConfigObservationOutcomeGap || page.Cursor != "good" {
			t.Fatal(page, e)
		}
		return false
	})
	c.ObservePagesContext(context.Background(), RunOverrides{}, "", 0)(func(page ConfigObservation, e error) bool {
		if e != nil || page.Cursor != "fresh" {
			t.Fatal(page, e)
		}
		return false
	})
	if e := <-served; e != nil {
		t.Fatal(e)
	}
}

func TestObserverPagesBoundsBeforeTransport(t *testing.T) {
	c := NewObserver("must-not-open")
	dialed := false
	c.transport.Dialer = func(context.Context, string) (net.Conn, error) {
		dialed = true
		return nil, errors.New("unexpected observation transport")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		ctx       context.Context
		cursor    string
		wait      int64
		overrides RunOverrides
	}{
		{ctx: ctx}, {ctx: context.Background(), wait: -1},
		{ctx: context.Background(), wait: 30001},
		{ctx: context.Background(), cursor: string(make([]byte, 513))},
		{ctx: context.Background(), cursor: string([]byte{0xff})},
		{ctx: context.Background(), overrides: RunOverrides{Store: string(make([]byte, 4097))}},
	} {
		n := 0
		c.ObservePagesContext(tc.ctx, tc.overrides, tc.cursor, tc.wait)(func(_ ConfigObservation, e error) bool {
			n++
			if e == nil {
				t.Fatal("invalid request admitted")
			}
			if tc.ctx == ctx && !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			return true
		})
		if n != 1 {
			t.Fatal(n)
		}
		if dialed {
			t.Fatal("invalid observation reached transport")
		}
	}
}
