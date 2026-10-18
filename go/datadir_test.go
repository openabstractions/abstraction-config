package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No shipped default resolves under the home folder root, which File History
// backs up whole (owner, 2026-10-06; the old ~/.abstraction job store).
func TestDataDirIsNeverTheHomeFolderRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	local := filepath.Join(t.TempDir(), "local")
	env := map[string]string{"LOCALAPPDATA": local}
	getenv := func(name string) string { return env[name] }
	homeDir := func() (string, error) { return home, nil }
	want := map[string]string{
		"windows": filepath.Join(local, "openabstractions"),
		"darwin":  filepath.Join(home, "Library", "Application Support", "openabstractions"),
		"linux":   filepath.Join(home, ".local", "share", "openabstractions"),
	}
	for platform, expected := range want {
		got, err := DataDirFor(platform, getenv, homeDir)
		if err != nil || got != expected {
			t.Fatalf("%s: DataDirFor = %q, %v; want %q", platform, got, err, expected)
		}
		rel, err := filepath.Rel(home, got)
		if err == nil && !strings.HasPrefix(rel, "..") && !strings.Contains(rel, string(filepath.Separator)) {
			t.Fatalf("%s: %q is directly in the home folder", platform, got)
		}
	}
	if _, err := DataDirFor("windows", func(string) string { return "" }, homeDir); err == nil {
		t.Fatal("windows without LOCALAPPDATA must refuse, never fall back to home")
	}
}

func TestLegacyHomeStoreNotice(t *testing.T) {
	home, store := t.TempDir(), filepath.Join(t.TempDir(), "job-store")
	if got := LegacyHomeStoreNotice(store, home); got != "" {
		t.Fatalf("no legacy store, notice %q", got)
	}
	legacy := filepath.Join(home, ".modelget")
	plant(t, legacy)
	got := LegacyHomeStoreNotice(store, home)
	if !strings.Contains(got, legacy) || !strings.Contains(got, "ABSTRACTION_STORE") || strings.Contains(got, "\n") {
		t.Fatalf("notice %q", got)
	}
	plant(t, store)
	if got := LegacyHomeStoreNotice(store, home); got != "" {
		t.Fatalf("new store in use, notice %q", got)
	}
}

func plant(t *testing.T, store string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(store, "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "jobs", "a.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyJobStoreAndUserPathAvoidTheHomeFolderRoot(t *testing.T) {
	t.Setenv("LOCALAPPDATA", filepath.Join(t.TempDir(), "local"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdgdata"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	store, err := LegacyJobStore()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{store, UserPath()} {
		if rel, err := filepath.Rel(home, path); err == nil {
			first := strings.Split(rel, string(filepath.Separator))[0]
			if strings.HasPrefix(first, ".abstraction") || first == ".modelget" {
				t.Fatalf("%q resolves under the home folder root", path)
			}
		}
	}
	if filepath.Base(store) != "job-store" {
		t.Fatalf("LegacyJobStore = %q", store)
	}
}
