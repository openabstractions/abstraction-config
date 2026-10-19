package client

import (
	"testing"
	"time"

	"github.com/openabstractions/abstraction-identity/listen"
)

// Every client waits DefaultTimeout, two seconds, when its transport names no
// timeout, and keeps a timeout the transport names. A service that consults a
// policy before answering answers within this wait (CONTRACT.md, client wait).
func TestClientsWaitTheDocumentedDefault(t *testing.T) {
	if DefaultTimeout != 2*time.Second {
		t.Fatalf("DefaultTimeout is %v; CONTRACT.md documents two seconds", DefaultTimeout)
	}
	for name, got := range map[string]time.Duration{
		"reader":   New("endpoint").transport.Timeout,
		"editor":   NewEditor("endpoint").transport.Timeout,
		"observer": NewObserver("endpoint").transport.Timeout,
	} {
		if got != DefaultTimeout {
			t.Fatalf("%s waits %v, want DefaultTimeout", name, got)
		}
	}
	if got := NewWithTransport(listen.FrameClient{Endpoint: "endpoint", Timeout: time.Second}).transport.Timeout; got != time.Second {
		t.Fatalf("an explicit timeout became %v", got)
	}
}

// An observation's call limit covers its wait plus ObserveMargin, so the
// two-second default never ends a long wait before the service answers
// (CFG-W12). A longer transport limit stays; a zero wait keeps the default.
func TestObserveCallLimitCoversItsWait(t *testing.T) {
	short := New("endpoint").transport
	if got := observeTransport(short, 25000).Timeout; got != 25*time.Second+ObserveMargin {
		t.Fatalf("a 25 s observation waits %v", got)
	}
	if got := observeTransport(short, 0).Timeout; got != DefaultTimeout {
		t.Fatalf("a zero wait waits %v", got)
	}
	long := listen.FrameClient{Endpoint: "endpoint", Timeout: time.Minute}
	if got := observeTransport(long, 25000).Timeout; got != time.Minute {
		t.Fatalf("an explicit longer limit became %v", got)
	}
	if short.Timeout != DefaultTimeout {
		t.Fatal("deriving an observation's limit changed the client's own")
	}
}
