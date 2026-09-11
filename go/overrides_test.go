package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitOverridesDoNotReadHostRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("ProgramData", filepath.Join(home, "machine"))
	if err := Save(UserPath(), Config{Store: "file-value"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ABSTRACTION_STORE", "host-value")
	got := LoadWithOverrides(nil)
	if got.Store != "file-value" || got.Origin("store").Rung != User {
		t.Fatalf("host run leaked: %+v", got)
	}
	got = LoadWithOverrides(map[string]string{"ABSTRACTION_STORE": "caller-value"})
	if got.Store != "caller-value" || got.Origin("store").Rung != Environment {
		t.Fatalf("explicit override missing: %+v", got)
	}
	if Load().Store != os.Getenv("ABSTRACTION_STORE") {
		t.Fatal("legacy local Load behavior changed")
	}
}
