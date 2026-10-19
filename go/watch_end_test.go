//go:build windows || linux || darwin || freebsd || netbsd || openbsd || dragonfly

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	watch "github.com/openabstractions/abstraction-watch/go"
)

// WatchFile (W0-06(e)) shares the death report of WatchInvalidations
// (W0-06(n)): deleting the file's directory closes Changes with a cause.
func TestWatchFileReportsDirectoryDeletion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := WatchFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(settle * 10)
	for {
		select {
		case _, ok := <-s.Changes():
			if ok {
				continue
			}
			if !errors.Is(s.Err(), watch.ErrRootGone) {
				t.Fatalf("Err = %v", s.Err())
			}
			return
		case <-deadline:
			t.Fatal("deleted file directory left the WatchFile subscription open")
		}
	}
}

func TestInvalidationsReportDirectoryDeletion(t *testing.T) {
	path := own(t)
	s, err := WatchInvalidations()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(settle * 10)
	for {
		select {
		case _, ok := <-s.Changes():
			if ok {
				continue
			}
			if !errors.Is(s.Err(), watch.ErrRootGone) {
				t.Fatalf("Err = %v", s.Err())
			}
			return
		case <-deadline:
			t.Fatal("deleted watched directory left the subscription open")
		}
	}
}
