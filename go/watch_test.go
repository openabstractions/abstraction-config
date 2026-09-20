package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// own points this machine's answers at a directory the test owns, so that a
// test never watches, and never writes to, the machine it runs on.
func own(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", dir)
		t.Setenv("ProgramData", filepath.Join(dir, "machine"))
	} else if runtime.GOOS == "darwin" {
		t.Setenv("HOME", dir)
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	path := UserPath()
	if rel, err := filepath.Rel(dir, path); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatalf("user configuration escaped test home: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// attach subscribes to invalidations the way the config service does.
func attach(t *testing.T) *InvalidationSubscription {
	t.Helper()
	s, err := WatchInvalidations()
	if err != nil {
		t.Fatalf("native watcher unavailable: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// expect waits for invalidations until a reread of the files, with no process
// overrides, holds want. Signals coalesce, so one signal may cover several
// writes and a write may produce several signals.
func expect(t *testing.T, s *InvalidationSubscription, want func(Config) bool, what string) {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-s.Changes():
			if want(LoadWithOverrides(nil)) {
				return
			}
		case <-deadline:
			t.Fatalf("no invalidation led to %s; last read %+v", what, LoadWithOverrides(nil))
		}
	}
}

func storeIs(value string) func(Config) bool {
	return func(c Config) bool { return c.Store == value }
}

func TestExistingFileEditsSurviveReplacementAndDeletion(t *testing.T) {
	path := own(t)
	write := func(at, value string) {
		t.Helper()
		if err := os.WriteFile(at, []byte(`{"store":"`+value+`"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Seed before subscribing: directory-only kqueue watches cannot detect the
	// following in-place write to an already-existing inode.
	write(path, "initial")
	s := attach(t)
	write(path, "in-place")
	expect(t, s, storeIs("in-place"), "the in-place edit")
	// The replacement must be watched by its new inode, not the old handle.
	replacement := path + ".new"
	write(replacement, "replacement")
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	expect(t, s, storeIs("replacement"), "the replacement")
	write(path, "replacement-edited")
	expect(t, s, storeIs("replacement-edited"), "the edited replacement")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	expect(t, s, storeIs(""), "the deletion")
	write(path, "recreated")
	expect(t, s, storeIs("recreated"), "the recreation")
	write(path, "recreated-edited")
	expect(t, s, storeIs("recreated-edited"), "the edited recreation")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAnEditThroughThisLayerIsReportedToASubscriber(t *testing.T) {
	path := own(t)
	s := attach(t)
	if err := Edit(path, func(c *Config) error { c.NASStore = `\\nas\models`; return nil }); err != nil {
		t.Fatal(err)
	}
	expect(t, s, func(c Config) bool { return c.NASStore == `\\nas\models` }, "the edit")
}

func TestAnEditByAProgramThatNeverHeardOfUsIsReported(t *testing.T) {
	path := own(t)
	s := attach(t)
	if err := os.WriteFile(path, []byte(`{"store":"D:\\jobs"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	expect(t, s, storeIs(`D:\jobs`), "the foreign write")
}

// A text editor writes several times to save once; the reread after the burst
// is the answer that was left behind.
func TestABurstOfWritesIsReportedAsTheAnswerItLeft(t *testing.T) {
	path := own(t)
	s := attach(t)
	for _, store := range []string{"one", "two", "three", "settled"} {
		if err := os.WriteFile(path, []byte(`{"store":"`+store+`"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	expect(t, s, storeIs("settled"), "the settled answer")
}

// A subscriber that never reads, and one that has gone, must not stop a writer.
func TestAWriterIsNotHeldUpByASubscriber(t *testing.T) {
	path := own(t)
	idle := attach(t)
	idle.Changes()
	gone := attach(t)
	gone.Close()
	for i := range 50 {
		if err := Edit(path, func(c *Config) error { c.Store = string(rune('a' + i%26)); return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheLayerWorksWithNobodySubscribed(t *testing.T) {
	path := own(t)
	if err := Save(path, Config{Store: "alone"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadWithOverrides(nil).Store; got != "alone" {
		t.Fatalf("read back %q", got)
	}
}
