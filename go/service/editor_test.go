package service

import (
	"context"
	"errors"
	"fmt"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-config/go/client"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func editorHost(t *testing.T, wrongUser bool) (*client.Editor, *client.Client) {
	t.Helper()
	endpoint := listen.Endpoint(fmt.Sprintf("config-editor-%d-%d", os.Getpid(), serial.Add(1)))
	h, err := Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if wrongUser {
		h.owner = "another-account"
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(context.Background()) }()
	t.Cleanup(func() {
		h.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(time.Second):
			t.Error("editor did not stop")
		}
	})
	return client.NewEditor(endpoint), client.New(endpoint)
}
func TestUserEditorConcurrentReplacement(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	t.Setenv("ABSTRACTION_STORE", "host-environment")
	editor, reader := editorHost(t, false)
	initial, err := editor.ReadUser()
	if err != nil {
		t.Fatal(err)
	}
	if initial.Values.Store != "" || initial.Revision == "" {
		t.Fatalf("merged host environment: %+v", initial)
	}
	if _, err := os.Stat(config.UserPath()); !os.IsNotExist(err) {
		t.Fatalf("read created file: %v", err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan wire.UserReplaceResult, 2)
	failures := make(chan error, 2)
	start := make(chan struct{})
	second := *editor
	for index, value := range []string{"editor-one", "editor-two"} {
		binding := editor
		if index == 1 {
			binding = &second
		}
		wg.Add(1)
		go func(value string) {
			defer wg.Done()
			<-start
			r, err := binding.ReplaceUser(initial.Revision, wire.UserSettings{Store: value, Off: map[string]string{"nas": "paused"}})
			outcomes <- r
			failures <- err
		}(value)
	}
	close(start)
	wg.Wait()
	close(outcomes)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	applied, conflicts := 0, 0
	var winner wire.UserSnapshot
	for r := range outcomes {
		switch r.Outcome {
		case wire.UserReplaceOutcomeApplied:
			applied++
			winner = r.Snapshot
		case wire.UserReplaceOutcomeConflict:
			conflicts++
		default:
			t.Fatalf("bad outcome: %+v", r)
		}
	}
	if applied != 1 || conflicts != 1 {
		t.Fatalf("applied=%d conflict=%d", applied, conflicts)
	}
	current, err := editor.ReadUser()
	if err != nil || current.Revision != winner.Revision {
		t.Fatalf("winner %+v %v", current, err)
	}
	stale, err := editor.ReplaceUser(initial.Revision, wire.UserSettings{})
	if err != nil || stale.Outcome != wire.UserReplaceOutcomeConflict || stale.Snapshot.Revision != current.Revision {
		t.Fatalf("stale %+v %v", stale, err)
	}
	merged, err := reader.ReadWithOverrides(wire.RunOverrides{Store: "caller-run"})
	if err != nil || merged.Store != "caller-run" {
		t.Fatalf("reader changed %+v %v", merged, err)
	}
	user, err := editor.ReadUser()
	if err != nil || user.Values.Store != winner.Values.Store {
		t.Fatalf("run claims entered user rung %+v %v", user, err)
	}
	cleared, err := editor.ReplaceUser(current.Revision, wire.UserSettings{})
	if err != nil || cleared.Outcome != wire.UserReplaceOutcomeApplied || cleared.Snapshot.Revision != initial.Revision {
		t.Fatalf("clear %+v %v", cleared, err)
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := editor.ReplaceUserContext(expired, cleared.Snapshot.Revision, wire.UserSettings{Store: "must-not-write"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
func TestUserEditorRefusesStorageAndWrongUser(t *testing.T) {
	requireProgramProof(t)
	for _, body := range []string{"", "null", "{", `{"future":true}`, `{} {}`, `{"store":42}`, `{"store":null}`, `{"off":null}`, `{"off":{"nas":null}}`, `{"STORE":"case"}`, `{"store":"one","store":"two"}`, `{"store":"\ud800"}`, `{"store":"\udc00"}`, "{\"store\":\"" + string([]byte{0xff}) + "\"}"} {
		t.Run(fmt.Sprintf("corrupt-%q", body), func(t *testing.T) {
			isolated(t)
			path := config.UserPath()
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			editor, _ := editorHost(t, false)
			for _, operation := range []func() error{func() error { _, err := editor.ReadUser(); return err }, func() error { _, err := editor.ReplaceUser("stale", wire.UserSettings{}); return err }} {
				var serviceError *wire.ServiceError
				if err := operation(); !errors.As(err, &serviceError) || serviceError.Code != "storage_unavailable" {
					t.Fatalf("storage refusal: %v", err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != body {
				t.Fatalf("corruption clobbered: %q %v", after, err)
			}
		})
	}
	t.Run("oversized-storage", func(t *testing.T) {
		isolated(t)
		path := config.UserPath()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, config.MaxUserFileBytes+1)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		editor, _ := editorHost(t, false)
		var refusal *wire.ServiceError
		if _, err := editor.ReadUser(); !errors.As(err, &refusal) || refusal.Code != "storage_unavailable" {
			t.Fatalf("oversized read: %v", err)
		}
		if _, err := editor.ReplaceUser("old", wire.UserSettings{}); !errors.As(err, &refusal) || refusal.Code != "storage_unavailable" {
			t.Fatalf("oversized replace: %v", err)
		}
		if info, err := os.Stat(path); err != nil || info.Size() != int64(len(body)) {
			t.Fatalf("oversized record changed: %v", err)
		}
	})
	t.Run("storage-is-directory", func(t *testing.T) {
		isolated(t)
		path := config.UserPath()
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
		editor, _ := editorHost(t, false)
		var refusal *wire.ServiceError
		if _, err := editor.ReadUser(); !errors.As(err, &refusal) || refusal.Code != "storage_unavailable" {
			t.Fatalf("unreadable storage: %v", err)
		}
		if _, err := editor.ReplaceUser("revision", wire.UserSettings{}); !errors.As(err, &refusal) || refusal.Code != "storage_unavailable" {
			t.Fatalf("unwritable storage: %v", err)
		}
	})
	t.Run("wrong-user", func(t *testing.T) {
		isolated(t)
		editor, _ := editorHost(t, true)
		var refusal *wire.ServiceError
		if _, err := editor.ReplaceUser("anything", wire.UserSettings{Store: "forbidden"}); !errors.As(err, &refusal) || refusal.Code != "wrong_user" {
			t.Fatalf("wrong user: %v", err)
		}
		if _, err := os.Stat(filepath.Dir(config.UserPath())); !os.IsNotExist(err) {
			t.Fatalf("forbidden caller touched storage: %v", err)
		}
	})
	t.Run("path-is-captured", func(t *testing.T) {
		isolated(t)
		editor, _ := editorHost(t, false)
		initial, err := editor.ReadUser()
		if err != nil {
			t.Fatal(err)
		}
		path := config.UserPath()
		other := t.TempDir()
		t.Setenv("APPDATA", other)
		t.Setenv("XDG_CONFIG_HOME", other)
		t.Setenv("HOME", other)
		if _, err := editor.ReplaceUser(initial.Revision, wire.UserSettings{Store: "selected-once"}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(config.UserPath()); !os.IsNotExist(err) {
			t.Fatalf("host path drift: %v", err)
		}
	})
}
