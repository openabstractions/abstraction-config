package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDirectoryNotificationIsArmedBeforeReturn(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	dir := t.TempDir()
	events, stop, err := notifyDirs([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	// With one P, write immediately, before a newly launched reporter can arm.
	if err := os.WriteFile(filepath.Join(dir, "external.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("notification was not armed when notifyDirs returned")
	}
}

func TestDirectoryNotificationCloseWithoutWriter(t *testing.T) {
	for i := 0; i < 30; i++ {
		_, stop, err := notifyDirs([]string{t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() { stop(); stop(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stop did not cancel and drain pending notification")
		}
	}
}

func TestDirectoryNotificationCloseRacesChanges(t *testing.T) {
	for i := 0; i < 20; i++ {
		dir := t.TempDir()
		events, stop, err := notifyDirs([]string{dir})
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() {
			for j := 0; j < 20; j++ {
				if err := os.WriteFile(filepath.Join(dir, "changing.json"), []byte("{}"), 0600); err != nil {
					finished <- err
					return
				}
			}
			finished <- nil
		}()
		select {
		case <-events:
		case <-time.After(time.Second):
			t.Fatal("first edit not observed")
		}
		stopped := make(chan struct{})
		go func() { stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("close/rearm race did not drain")
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		stop()
	}
}
