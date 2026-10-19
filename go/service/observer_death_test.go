package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
)

// Deleting or moving the watched configuration folder ends the native source.
// The host recreates the folder, watches it again and logs both steps; a later
// external write reaches an observer without a restart.
func TestConfigObserverRearmsWhenDirectoryDeleted(t *testing.T) {
	t.Run("deleted", func(t *testing.T) { observerRearms(t, os.RemoveAll) })
	t.Run("moved", func(t *testing.T) {
		observerRearms(t, func(dir string) error { return os.Rename(dir, dir+".moved") })
	})
}

func observerRearms(t *testing.T, remove func(dir string) error) {
	requireProgramProof(t)
	isolated(t)
	if e := config.Save(config.UserPath(), config.Config{Store: "first"}); e != nil {
		t.Fatal(e)
	}
	var logged struct {
		sync.Mutex
		lines []string
	}
	h, c, _ := observerHost(t, func(h *Host) {
		h.OnError = func(e error) {
			logged.Lock()
			logged.lines = append(logged.lines, e.Error())
			logged.Unlock()
		}
	})
	first, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil || first.Outcome != wire.ConfigObservationOutcomeSnapshot || first.Snapshot.Store != "first" {
		t.Fatal(first, e)
	}
	dir := filepath.Dir(config.UserPath())
	if e := remove(dir); e != nil {
		t.Fatal(e)
	}
	// The folder's loss is itself a change: the waiter rereads and sees no store.
	gone, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, first.Cursor, 5000)
	if e != nil || gone.Outcome != wire.ConfigObservationOutcomeSnapshot || gone.Snapshot.Store != "" {
		t.Fatal(gone, e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, e := os.Stat(dir)
		if e == nil && info.IsDir() && h.ObservationAvailable() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("configuration folder not recreated and watched again", e)
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := make(chan wire.ConfigObservation, 1)
	go func() {
		cursor := gone.Cursor
		for {
			next, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, cursor, 30000)
			if e != nil || next.Outcome != wire.ConfigObservationOutcomeSnapshot || next.Snapshot.Store == "after" {
				got <- next
				return
			}
			cursor = next.Cursor
		}
	}()
	waitConfigObservers(t, h, 1)
	writeExternally(t, config.UserPath(), `{"store":"after"}`)
	select {
	case next := <-got:
		if next.Snapshot == nil || next.Snapshot.Store != "after" {
			t.Fatalf("unexpected observation after rearm: %+v", next)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("external write after the folder came back was unseen")
	}
	logged.Lock()
	defer logged.Unlock()
	joined := strings.Join(logged.lines, "\n")
	if !strings.Contains(joined, "went away") || !strings.Contains(joined, "watching the configuration folder again") {
		t.Fatalf("repair not logged: %q", joined)
	}
}
