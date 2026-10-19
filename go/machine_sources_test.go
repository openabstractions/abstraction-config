package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	cas "github.com/openabstractions/abstraction-cas/go"
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

// The injected trust decision admits only this test's source; it changes no
// administrator file or operating-system ownership policy.
func TestMergedTrustedMachineReadFailureReturnsNoSettings(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		name := "directory"
		if oversized {
			name = "oversized"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			user := filepath.Join(root, "user.json")
			userBytes := []byte(`{"store":"USER-MUST-NOT-ESCAPE"}`)
			if err := os.WriteFile(user, userBytes, 0600); err != nil {
				t.Fatal(err)
			}
			machine := filepath.Join(root, "machine.json")
			if oversized {
				if err := os.WriteFile(machine, bytes.Repeat([]byte("x"), MaxUserFileBytes+1), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(machine, 0700); err != nil {
				t.Fatal(err)
			}
			checked := 0
			trust := func(path string) error {
				checked++
				if path != machine {
					t.Fatalf("wrong trust source %q", path)
				}
				return nil
			}
			got, err := loadWithSources(api.BoundedFileStore{MaxBytes: MaxUserFileBytes}, user, machine, trust,
				map[string]string{"ABSTRACTION_STORE": "RUN-MUST-NOT-ESCAPE"})
			if err == nil || !reflect.DeepEqual(got, Config{}) || checked != 1 {
				t.Fatalf("trusted unreadable machine returned settings: %+v, error=%v, trust checks=%d", got, err, checked)
			}
			if oversized && !errors.Is(err, cas.ErrTooLarge) {
				t.Fatalf("lost storage cause: %v", err)
			}
			after, readErr := os.ReadFile(user)
			if readErr != nil || !bytes.Equal(after, userBytes) {
				t.Fatal("machine refusal changed user source", readErr)
			}
		})
	}
}

func TestMergedMachineFallbacksAndPrecedenceStayUnchanged(t *testing.T) {
	for _, state := range []string{"untrusted", "missing", "malformed", "readable", "omitted"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			user, machine := filepath.Join(root, "user.json"), filepath.Join(root, "machine.json")
			if err := os.WriteFile(user, []byte(`{"store":"user"}`), 0600); err != nil {
				t.Fatal(err)
			}
			body := []byte(`{"store":"machine","nas_store":"machine-nas"}`)
			if state == "malformed" {
				body = []byte(`{"store":`)
			}
			if state != "missing" && state != "omitted" {
				if err := os.WriteFile(machine, body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if state == "omitted" {
				machine = ""
			}
			checked := 0
			trust := func(string) error {
				checked++
				if state == "untrusted" {
					return errors.New("fixture trust refused")
				}
				return nil
			}
			got, err := loadWithSources(api.BoundedFileStore{MaxBytes: MaxUserFileBytes}, user, machine, trust, map[string]string{"ABSTRACTION_STORE": "run"})
			if err != nil || got.Store != "run" {
				t.Fatal("run/user fallback changed", got, err)
			}
			wantNAS := ""
			if state == "readable" {
				wantNAS = "machine-nas"
			}
			if got.NASStore != wantNAS {
				t.Fatal("machine fallback changed", got, err)
			}
			wantChecks := 1
			if state == "omitted" {
				wantChecks = 0
			}
			if checked != wantChecks {
				t.Fatalf("trust calls=%d want%d", checked, wantChecks)
			}
		})
	}
}
