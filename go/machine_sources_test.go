package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	api "github.com/openabstractions/abstraction-cas/go/api"
)

// A loader given no machine path reads no machine rung: the trust check, the
// first thing a machine read does, never runs, and a planted machine value
// never appears. The same loader given the sentinel machine path reads it,
// which proves the sentinel is where a machine read looks.
func TestALoaderWithoutAMachinePathReadsNoMachineRung(t *testing.T) {
	sentinel := t.TempDir()
	machine := filepath.Join(sentinel, Name, "config.json")
	if runtime.GOOS == "windows" {
		t.Setenv("ProgramData", sentinel)
		if MachinePath() != machine {
			t.Fatalf("ProgramData did not move the machine rung: %s", MachinePath())
		}
	}
	if err := os.MkdirAll(filepath.Dir(machine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(machine, []byte(`{"store":"SENTINEL-MACHINE"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := api.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	user := filepath.Join(t.TempDir(), "config.json")
	var checked []string
	trust := func(path string) error { checked = append(checked, path); return nil }

	withMachine, err := loadWithSources(store, user, machine, trust, nil)
	if err != nil || withMachine.Store != "SENTINEL-MACHINE" || len(checked) == 0 {
		t.Fatalf("the sentinel machine rung was not read: %+v %v, trust checked %v", withMachine, err, checked)
	}
	checked = nil
	without, err := loadWithSources(store, user, "", trust, nil)
	if err != nil || without.Store != "" || len(checked) != 0 {
		t.Fatalf("a loader without a machine path read the machine rung: %+v %v, trust checked %v", without, err, checked)
	}
	checked = nil
	if _, err := LoadWithSources(store, user, "", nil); err != nil || len(checked) != 0 {
		t.Fatalf("LoadWithSources without a machine path: %v, trust checked %v", err, checked)
	}
}
