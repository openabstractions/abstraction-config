package config

import (
	"bytes"
	"errors"
	cas "github.com/openabstractions/abstraction-cas/go"
	api "github.com/openabstractions/abstraction-cas/go/api"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type racingStore struct {
	api.Store
	reads atomic.Int32
	moved atomic.Int32
	ready chan struct{}
}

func (s *racingStore) Read(path string) (api.Value, error) {
	v, e := s.Store.Read(path)
	n := s.reads.Add(1)
	if n <= 2 {
		if n == 2 {
			close(s.ready)
		}
		<-s.ready
	}
	return v, e
}
func (s *racingStore) Write(path string, base api.Value, data []byte) error {
	e := s.Store.Write(path, base, data)
	if errors.Is(e, cas.ErrMoved) {
		s.moved.Add(1)
	}
	return e
}
func TestUserStoreConcurrentComparisonAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	native := api.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	initial, e := ReadUserStore(native, path)
	if e != nil {
		t.Fatal(e)
	}
	store := &racingStore{Store: native, ready: make(chan struct{})}
	var wg sync.WaitGroup
	var applied atomic.Int32
	results := make(chan UserFileSnapshot, 2)
	for _, value := range []string{"one", "two"} {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			s, ok, e := ReplaceUserStore(store, path, initial.Revision, Config{Store: v})
			if e != nil {
				t.Error(e)
				return
			}
			if ok {
				applied.Add(1)
			}
			results <- s
		}(value)
	}
	wg.Wait()
	close(results)
	if applied.Load() != 1 || store.moved.Load() != 1 {
		t.Fatalf("applied=%d moved=%d", applied.Load(), store.moved.Load())
	}
	latest, e := ReadUserStore(api.BoundedFileStore{MaxBytes: MaxUserFileBytes}, path)
	if e != nil {
		t.Fatal(e)
	}
	for r := range results {
		if r.Revision != latest.Revision {
			t.Fatal("conflict did not return latest snapshot")
		}
	}
}

type failedStore struct {
	api.Store
	writeErr     error
	readData     []byte
	overrideRead bool
}

func (s failedStore) Read(path string) (api.Value, error) {
	if s.overrideRead {
		return api.Value{Data: s.readData}, nil
	}
	return s.Store.Read(path)
}
func (s failedStore) Write(string, api.Value, []byte) error { return s.writeErr }
func TestUserStoreFailurePreservesContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	original := []byte(`{"store":"before"}`)
	if e := os.WriteFile(path, original, 0600); e != nil {
		t.Fatal(e)
	}
	native := api.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	initial, e := ReadUserStore(native, path)
	if e != nil {
		t.Fatal(e)
	}
	failure := errors.New("injected storage failure")
	if s, ok, e := ReplaceUserStore(failedStore{Store: native, writeErr: failure}, path, initial.Revision, Config{Store: "after"}); !errors.Is(e, failure) || ok || s.Revision != "" {
		t.Fatal(s, ok, e)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatal("write failure changed content")
	}
	oversized := failedStore{overrideRead: true, readData: []byte(strings.Repeat("x", MaxUserFileBytes+1))}
	if _, e := ReadUserStore(oversized, "key"); !errors.Is(e, cas.ErrTooLarge) {
		t.Fatal(e)
	}
	if _, ok, e := ReplaceUserStore(native, path, initial.Revision, Config{Store: strings.Repeat("x", MaxUserFileBytes)}); !errors.Is(e, cas.ErrTooLarge) || ok {
		t.Fatal(ok, e)
	}
	got, _ = os.ReadFile(path)
	if !bytes.Equal(got, original) {
		t.Fatal("size refusal changed content")
	}
	for _, raw := range [][]byte{nil, {}, []byte("corrupt")} {
		s, e := ReadUserStore(failedStore{overrideRead: true, readData: raw}, "key")
		if raw == nil && e != nil {
			t.Fatal(e)
		}
		if raw != nil && e == nil {
			t.Fatal(s)
		}
	}
}

type spellingChangeStore struct {
	api.Store
	writes int
}

func (s *spellingChangeStore) Write(path string, base api.Value, data []byte) error {
	s.writes++
	if s.writes == 1 {
		if e := s.Store.Write(path, base, []byte("{  }")); e != nil {
			return e
		}
		return cas.ErrMoved
	}
	return s.Store.Write(path, base, data)
}
func TestUserStoreMovedEquivalentContentRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	native := api.BoundedFileStore{MaxBytes: MaxUserFileBytes}
	initial, e := ReadUserStore(native, path)
	if e != nil {
		t.Fatal(e)
	}
	store := &spellingChangeStore{Store: native}
	got, applied, e := ReplaceUserStore(store, path, initial.Revision, Config{Store: "after"})
	if e != nil || !applied || got.Values.Store != "after" || store.writes != 2 {
		t.Fatal(got, applied, e, store.writes)
	}
}
