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
	got, err := read(path)
	if err != nil || got.NASStore != `\\nas\models` || got.From != path {
		t.Fatalf("read back %+v, %v", got, err)
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
	c, err := read(path)
	if err != nil || len(c.Store)%100 != 0 {
		t.Fatalf("torn or unreadable: %v %d", err, len(c.Store))
	}
}

func TestMissingFileIsNotAnError(t *testing.T) {
	if _, err := read(filepath.Join(t.TempDir(), "none.json")); !os.IsNotExist(err) {
		t.Fatalf("got %v", err)
	}
}
