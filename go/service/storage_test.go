package service

import (
	"context"
	"errors"
	"fmt"
	api "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-config/go/client"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type selectedStore struct {
	api.Store
	reads atomic.Int32
	fail  atomic.Bool
}

func (s *selectedStore) Read(path string) (api.Value, error) {
	s.reads.Add(1)
	if s.fail.Load() {
		return api.Value{}, errors.New("selected backend unavailable")
	}
	return s.Store.Read(path)
}
func TestSelectedCASReaderEditorAndAuthorization(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	if err := config.Save(config.UserPath(), config.Config{Store: "unselected"}); err != nil {
		t.Fatal(err)
	}
	store := &selectedStore{Store: api.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}}
	path := filepath.Join(t.TempDir(), "selected.json")
	start := func(denied bool) (*client.Editor, *client.Client, func()) {
		endpoint := listen.Endpoint(fmt.Sprintf("config-selected-%d-%d", os.Getpid(), serial.Add(1)))
		h, e := ListenWithStore(endpoint, store, path)
		if e != nil {
			t.Fatal(e)
		}
		if denied {
			h.owner = "not-current-account"
		}
		done := make(chan error, 1)
		go func() { done <- h.Serve(context.Background()) }()
		stop := func() {
			h.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("service drain timeout")
			}
		}
		return client.NewEditor(endpoint), client.New(endpoint), stop
	}
	var revision string
	func() {
		editor, reader, stop := start(false)
		defer stop()
		initial, e := editor.ReadUser()
		if e != nil {
			t.Fatal(e)
		}
		if initial.Values.Store != "" {
			t.Fatal("read unselected file")
		}
		replacement, e := editor.ReplaceUser(initial.Revision, wire.UserSettings{Store: "selected", Off: map[string]string{}})
		if e != nil || replacement.Outcome != "applied" {
			t.Fatal(replacement, e)
		}
		revision = replacement.Snapshot.Revision
		got, e := reader.Read()
		if e != nil || got.Store != "selected" || got.Origins.Store.Rung != config.User || got.Origins.Store.Path != path {
			t.Fatal(got, e)
		}
		store.fail.Store(true)
		if _, e = reader.Read(); e == nil {
			t.Fatal("backend failure silently substituted other file")
		}
		if _, e = editor.ReadUser(); e == nil {
			t.Fatal("editor hid backend failure")
		}
		store.fail.Store(false)
	}()
	func() {
		editor, _, stop := start(false)
		defer stop()
		got, e := editor.ReadUser()
		if e != nil || got.Revision != revision || got.Values.Store != "selected" {
			t.Fatal(got, e)
		}
	}()
	func() {
		before := store.reads.Load()
		editor, reader, stop := start(true)
		defer stop()
		if _, e := editor.ReadUser(); e == nil {
			t.Fatal("denied editor")
		}
		if _, e := reader.Read(); e == nil {
			t.Fatal("denied reader")
		}
		if store.reads.Load() != before {
			t.Fatal("unauthorized caller reached storage")
		}
	}()
}
