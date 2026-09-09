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
	} else {
		t.Setenv("XDG_CONFIG_HOME", dir)
	}
	path := UserPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// attach subscribes and takes the opening notice, which is the present rather
// than a change.
func attach(t *testing.T) (*Subscription, <-chan Config) {
	t.Helper()
	s := Watch()
	t.Cleanup(func() { s.Close() })
	ch := s.Changes()
	told(t, s, ch)
	return s, ch
}

func told(t *testing.T, s *Subscription, ch <-chan Config) Config {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(10 * time.Second):
		t.Fatalf("nothing was reported; told by %s", s.How())
		return Config{}
	}
}

func TestAnEditThroughThisLayerIsReportedToASubscriber(t *testing.T) {
	path := own(t)
	s, ch := attach(t)
	if err := Edit(path, func(c *Config) error { c.NASStore = `\\nas\models`; return nil }); err != nil {
		t.Fatal(err)
	}
	if got := told(t, s, ch).NASStore; got != `\\nas\models` {
		t.Fatalf("reported %q", got)
	}
}

func TestAnEditByAProgramThatNeverHeardOfUsIsReported(t *testing.T) {
	path := own(t)
	s, ch := attach(t)
	if s.How() == "" {
		t.Fatal("How says nothing about the mechanism")
	}
	// A platform that has one must be using it: falling back to asking here
	// would pass this test and hide the thing it exists to prove.
	if notifier != "" && !strings.HasPrefix(s.How(), notifier) {
		t.Fatalf("%s is available and this subscription is %s", notifier, s.How())
	}
	if err := os.WriteFile(path, []byte(`{"store":"D:\\jobs"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := told(t, s, ch).Store; got != `D:\jobs` {
		t.Fatalf("reported %q; told by %s", got, s.How())
	}
}

// A text editor writes several times to save once, and the first thing a
// subscriber is told must be the answer that was left behind rather than one of
// the steps on the way to it.
func TestABurstOfWritesIsReportedAsTheAnswerItLeft(t *testing.T) {
	path := own(t)
	s, ch := attach(t)
	for _, store := range []string{"one", "two", "three", "settled"} {
		if err := os.WriteFile(path, []byte(`{"store":"`+store+`"}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if got := told(t, s, ch).Store; got != "settled" {
		t.Fatalf("first report was %q, which is a step and not the answer", got)
	}
}

func TestTheSameAnswerWrittenAgainIsNotAChange(t *testing.T) {
	path := own(t)
	if err := Edit(path, func(c *Config) error { c.Store = "same"; return nil }); err != nil {
		t.Fatal(err)
	}
	s, ch := attach(t)
	if err := Edit(path, func(c *Config) error { c.Store = "same"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := Edit(path, func(c *Config) error { c.Store = "different"; return nil }); err != nil {
		t.Fatal(err)
	}
	select {
	case c := <-ch:
		if c.Store != "different" {
			t.Fatalf("reported %q for a write that changed nothing", c.Store)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("nothing was reported; told by %s", s.How())
	}
}

// A subscriber that never reads, and one that has gone, must not stop a writer.
func TestAWriterIsNotHeldUpByASubscriber(t *testing.T) {
	path := own(t)
	idle := Watch()
	defer idle.Close()
	idle.Changes()
	gone := Watch()
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
	if got := Load().Store; got != "alone" {
		t.Fatalf("read back %q", got)
	}
}

func TestCurrentIsTheAnswerBeforeAnythingHasChanged(t *testing.T) {
	path := own(t)
	if err := Save(path, Config{Store: "already"}); err != nil {
		t.Fatal(err)
	}
	s := Watch()
	defer s.Close()
	if got := s.Current().Store; got != "already" {
		t.Fatalf("a fresh subscription says %q", got)
	}
}
