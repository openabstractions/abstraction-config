package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

// An empty profile has no configuration directory yet. The observer is still
// ready, and an external write of the user file is seen within one second
// (config P0-3).
func TestConfigObserverEmptyProfileSeesExternalWrite(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	h, c, _ := observerHost(t, nil)
	if !h.PrepareObservation() {
		t.Fatal("observer not ready from an empty profile")
	}
	first, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil || first.Outcome != wire.ConfigObservationOutcomeSnapshot {
		t.Fatal(first, e)
	}
	type result struct {
		store string
		e     error
	}
	got := make(chan result, 1)
	go func() {
		next, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, first.Cursor, 30000)
		r := result{e: e}
		if next.Snapshot != nil {
			r.store = next.Snapshot.Store
		}
		got <- r
	}()
	waitConfigObservers(t, h, 1)
	writeExternally(t, config.UserPath(), `{"store":"external"}`)
	select {
	case r := <-got:
		if r.e != nil || r.store != "external" {
			t.Fatalf("unexpected observation: %q %v", r.store, r.e)
		}
	case <-time.After(time.Second):
		t.Fatal("external write unseen within one second")
	}
}

// An existing one-file store rewritten in place (truncate and write, no
// rename) is seen within one second. kqueue reports this only on the file.
func TestFileObservationSeesInPlaceRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config.json")
	writeExternally(t, path, `{"store":"before"}`)
	changes, stop, e := FileObservation(path)(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := f.WriteString(`{"store":"after"}`); e != nil {
		t.Fatal(e)
	}
	if e := f.Close(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("in-place rewrite unseen within one second")
	}
}

// A selected one-file store under a missing directory is observed the same
// way, with no polling.
func TestFileObservationSeesExternalWriteUnderMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "config.json")
	changes, stop, e := FileObservation(path)(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	writeExternally(t, path, `{"store":"external"}`)
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("external write unseen within one second")
	}
}

func writeExternally(t *testing.T, path, body string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0o700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(body), 0o600); e != nil {
		t.Fatal(e)
	}
}
