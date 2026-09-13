package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestUserSnapshotStrictStringsAndNormalization(t *testing.T) {
	first, err := userSnapshot([]byte(`{"store":"\ud83d\ude00","off":{"nas":"paused"}}`))
	if err != nil {
		t.Fatal(err)
	}
	second, err := userSnapshot([]byte("{\n\"off\":{\"nas\":\"paused\"},\"store\":\"\U0001f600\"}\n"))
	if err != nil || first.Revision != second.Revision {
		t.Fatalf("equivalent normalized content: %+v %v", second, err)
	}
	missing, err := userSnapshot(nil)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := userSnapshot([]byte(`{"off":{},"store":""}`))
	if err != nil || missing.Revision != empty.Revision {
		t.Fatalf("empty normalization: %+v %v", empty, err)
	}
}

func TestUserEditorPreservesInvalidEarlierMapValue(t *testing.T) {
	for _, body := range []string{`{"off":{"nas":null,"nas":"ok"}}`, `{"off":{"nas":42,"nas":"ok"}}`, `{"off":{"nas":{},"nas":"ok"}}`} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadUserFile(path); err == nil {
			t.Fatalf("invalid earlier member accepted: %s", body)
		}
		if _, _, err := ReplaceUserFile(path, "revision", Config{}); err == nil {
			t.Fatal("invalid map replaced")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, []byte(body)) {
			t.Fatalf("bytes changed: %q %v", after, err)
		}
	}
	snapshot, err := userSnapshot([]byte(`{"off":{"nas":"old","nas":"new"}}`))
	if err != nil || snapshot.Values.Off["nas"] != "new" {
		t.Fatalf("valid last-wins changed: %+v %v", snapshot, err)
	}
}
