package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestSaveKeepsTheFormatAndLeavesNoTemporary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abstraction", "config.json")
	if err := Save(path, Config{NASStore: `\\nas\models`, LogSink: "/var/log/a.jsonl"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"nas_store\": \"\\\\\\\\nas\\\\models\",\n  \"log_sink\": \"/var/log/a.jsonl\"\n}\n"
	if string(b) != want {
		t.Fatalf("on-disk format changed:\n%s", b)
	}
	got, err := read(path, User)
	if err != nil || got.NASStore != `\\nas\models` || got.Origin("nas_store") != (Origin{User, path}) {
		t.Fatalf("read back %+v, %v", got, err)
	}
	if got.Origin("store") != (Origin{Rung: Default}) {
		t.Fatalf("a key this file does not set came from %v", got.Origin("store"))
	}
	names, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*"))
	if len(names) != 2 || !strings.HasSuffix(names[1], ".lock") {
		t.Fatalf("beside the file: %v", names)
	}
}

func TestConcurrentSavesEachLeaveAWholeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if err := Save(path, Config{Store: strings.Repeat("x", 100*(i+1))}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	c, err := read(path, User)
	if err != nil || len(c.Store)%100 != 0 {
		t.Fatalf("torn or unreadable: %v %d", err, len(c.Store))
	}
}

// The path half of provenance, which the corpus cannot carry: a path is one
// machine's, so no transcript two machines compare byte for byte can name one.
func TestOneVariableDoesNotMakeEveryKeyComeFromTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("ProgramData", filepath.Join(dir, "none"))
	for _, key := range Keys {
		if v := EnvVars[key]; v != "" {
			t.Setenv(v, "")
		}
	}
	user := UserPath()
	if err := Save(user, Config{Store: "A", LogSink: "L"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvVars["store"], "B")

	c := Load()
	if c.Store != "B" || c.Origin("store") != (Origin{Rung: Environment}) {
		t.Fatalf("store is %q from %v", c.Store, c.Origin("store"))
	}
	if c.LogSink != "L" || c.Origin("log_sink") != (Origin{User, user}) {
		t.Fatalf("one variable moved log_sink's provenance to %v", c.Origin("log_sink"))
	}
	if c.Origin("nas_store").Rung != Default {
		t.Fatalf("a key nothing set came from %v", c.Origin("nas_store"))
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	if _, err := read(filepath.Join(t.TempDir(), "none.json"), User); !os.IsNotExist(err) {
		t.Fatalf("got %v", err)
	}
}
