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

// Deleting the watched configuration directory ends the native source, and
// Observe answers unavailable within one settle period instead of waiting on a
// notification that can no longer arrive.
func TestConfigObserverUnavailableWhenDirectoryDeleted(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	if e := config.Save(config.UserPath(), config.Config{Store: "first"}); e != nil {
		t.Fatal(e)
	}
	h, c, _ := observerHost(t, nil)
	first, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil || first.Outcome != wire.ConfigObservationOutcomeSnapshot {
		t.Fatal(first, e)
	}
	if e := os.RemoveAll(filepath.Dir(config.UserPath())); e != nil {
		t.Fatal(e)
	}
	// One settle period is 100 ms; allow scheduling slack on a shared machine.
	deadline := time.Now().Add(time.Second)
	for h.ObservationAvailable() {
		if time.Now().After(deadline) {
			t.Fatal("deleted configuration directory left observation available")
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, first.Cursor, 0)
	if e != nil || got.Outcome != wire.ConfigObservationOutcomeUnavailable {
		t.Fatal(got, e)
	}
}
