package service

import (
	"context"
	"errors"
	"fmt"
	casapi "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	"github.com/openabstractions/abstraction-config/go/client"
	"github.com/openabstractions/abstraction-identity/listen"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func observerHost(t *testing.T, configure func(*Host)) (*Host, *client.Observer, *client.Editor) {
	t.Helper()
	requireProgramProof(t)
	ep := listen.Endpoint(fmt.Sprintf("config-observe-%d-%d", os.Getpid(), serial.Add(1)))
	h, e := Listen(ep)
	if e != nil {
		t.Fatal(e)
	}
	if configure != nil {
		configure(h)
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(context.Background()) }()
	t.Cleanup(func() {
		h.Close()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(3 * time.Second):
			t.Error("config observer failed to drain")
		}
	})
	c, e := client.NewObserver(ep).WithTimeout(35 * time.Second)
	if e != nil {
		t.Fatal(e)
	}
	return h, c, client.NewEditor(ep)
}
func waitConfigObservers(t *testing.T, h *Host, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.observation.mu.Lock()
		gate := h.observation.gate
		h.observation.mu.Unlock()
		count := 0
		if gate != nil {
			count = gate.Waiting()
		}
		if count == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("observation slots %d want %d", count, n)
		}
		runtime.Gosched()
	}
}
func TestConfigObserverNativeChangesLatestAndRestart(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	if e := config.Save(config.UserPath(), config.Config{Store: "first"}); e != nil {
		t.Fatal(e)
	}
	t.Setenv("ABSTRACTION_STORE", "host-must-not-mask-file-events")
	h, c, editor := observerHost(t, nil)
	if !h.ObservationAvailable() {
		t.Fatal("missing native observer")
	}
	first, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil || first.Outcome != wire.ConfigObservationOutcomeSnapshot || first.Snapshot.Store != "first" {
		t.Fatal(first, e)
	}
	type result struct {
		v wire.ConfigObservation
		e error
	}
	done := make(chan result, 1)
	go func() {
		v, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, first.Cursor, 30000)
		done <- result{v, e}
	}()
	waitConfigObservers(t, h, 1)
	replace := func(value string) {
		t.Helper()
		s, e := editor.ReadUserContext(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		s.Values.Store = value
		r, e := editor.ReplaceUserContext(context.Background(), s.Revision, s.Values)
		if e != nil || r.Outcome != wire.UserReplaceOutcomeApplied {
			t.Fatal(r, e)
		}
	}
	replace("second")
	var second wire.ConfigObservation
	select {
	case r := <-done:
		second = r.v
		if r.e != nil || second.Outcome != wire.ConfigObservationOutcomeSnapshot || second.Snapshot.Store != "second" {
			t.Fatal(second, r.e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native edit did not notify")
	}
	replace("intermediate")
	replace("latest")
	latest, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, second.Cursor, 30000)
	if e != nil || latest.Outcome != wire.ConfigObservationOutcomeSnapshot || latest.Snapshot.Store != "latest" {
		t.Fatal(latest, e)
	}
	unchanged, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, latest.Cursor, 5)
	if e != nil || unchanged.Outcome != wire.ConfigObservationOutcomeUnchanged || unchanged.Snapshot != nil {
		t.Fatal(unchanged, e)
	}
	gap, e := c.ObserveContext(context.Background(), wire.RunOverrides{Store: "caller"}, latest.Cursor, 0)
	if e != nil || gap.Outcome != wire.ConfigObservationOutcomeGap {
		t.Fatal(gap, e)
	}
	h.Close()
	_, fresh, _ := observerHost(t, nil)
	gap, e = fresh.ObserveContext(context.Background(), wire.RunOverrides{}, latest.Cursor, 0)
	if e != nil || gap.Outcome != wire.ConfigObservationOutcomeGap || gap.Snapshot != nil {
		t.Fatal(gap, e)
	}
}
func TestConfigObserverDisconnectCapacityDeadlineShutdown(t *testing.T) {
	isolated(t)
	changes := make(chan struct{}, 1)
	var mu sync.Mutex
	value := "initial"
	h, c, _ := observerHost(t, func(h *Host) {
		h.load = func(map[string]string) (config.Config, error) {
			mu.Lock()
			defer mu.Unlock()
			return config.Config{Store: value}, nil
		}
		if e := h.EnableObservation(func(context.Context) (<-chan struct{}, func(), error) { return changes, func() {}, nil }); e != nil {
			t.Fatal(e)
		}
	})
	initial, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 32)
	for i := 0; i < 32; i++ {
		go func() { _, e := c.ObserveContext(ctx, wire.RunOverrides{}, initial.Cursor, 30000); done <- e }()
	}
	waitConfigObservers(t, h, 32)
	full, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, initial.Cursor, 30000)
	if e != nil || full.Outcome != wire.ConfigObservationOutcomeUnavailable {
		t.Fatal(full, e)
	}
	cancel()
	for i := 0; i < 32; i++ {
		select {
		case e := <-done:
			if !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("client cancellation blocked")
		}
	}
	waitConfigObservers(t, h, 0)
	deadline, cancelDeadline := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelDeadline()
	go func() { _, e := c.ObserveContext(deadline, wire.RunOverrides{}, initial.Cursor, 30000); done <- e }()
	waitConfigObservers(t, h, 1)
	select {
	case e := <-done:
		if !errors.Is(e, context.DeadlineExceeded) && !os.IsTimeout(e) {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("deadline ignored")
	}
	waitConfigObservers(t, h, 0)
	mu.Lock()
	value = "latest"
	mu.Unlock()
	changes <- struct{}{}
	next, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, initial.Cursor, 30000)
	if e != nil || next.Snapshot == nil || next.Snapshot.Store != "latest" {
		t.Fatal(next, e)
	}
	go func() {
		_, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, next.Cursor, 30000)
		done <- e
	}()
	waitConfigObservers(t, h, 1)
	h.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("shutdown returned success")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown left wait alive")
	}
	waitConfigObservers(t, h, 0)
}
func TestConfigObserverUnsupportedAndInvalid(t *testing.T) {
	isolated(t)
	h, c, _ := observerHost(t, func(h *Host) { h.EnableObservation(nil) })
	if h.ObservationAvailable() {
		t.Fatal("unsupported advertised")
	}
	r, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil || r.Outcome != wire.ConfigObservationOutcomeUnsupported {
		t.Fatal(r, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.ObserveContext(ctx, wire.RunOverrides{}, "", 30000); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

func TestConfigObserverGeneratedPagesCancelAndBreak(t *testing.T) {
	isolated(t)
	changes := make(chan struct{})
	h, c, _ := observerHost(t, func(h *Host) {
		if e := h.EnableObservation(func(context.Context) (<-chan struct{}, func(), error) { return changes, func() {}, nil }); e != nil {
			t.Fatal(e)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pages := c.ObservePagesContext(ctx, wire.RunOverrides{}, "", 30000)
	count := 0
	pages(func(page wire.ConfigObservation, e error) bool {
		count++
		if e != nil || page.Snapshot == nil || page.Cursor == "" {
			t.Fatal(page, e)
		}
		return false
	})
	if count != 1 {
		t.Fatal(count)
	}
	waitConfigObservers(t, h, 0)
	done := make(chan error, 1)
	go func() {
		pages(func(_ wire.ConfigObservation, e error) bool { done <- e; return false })
	}()
	waitConfigObservers(t, h, 1)
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("generated observation did not cancel")
	}
	waitConfigObservers(t, h, 0)
}

func TestConfigObserverSelectedStoreRequiresItsOwnSource(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	if e := config.Save(config.UserPath(), config.Config{Store: "unselected"}); e != nil {
		t.Fatal(e)
	}
	store := &selectedStore{Store: casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}}
	path := filepath.Join(t.TempDir(), "selected.json")
	endpoint := listen.Endpoint(fmt.Sprintf("config-observe-selected-%d-%d", os.Getpid(), serial.Add(1)))
	h, e := ListenWithStore(endpoint, store, path)
	if e != nil {
		t.Fatal(e)
	}
	if h.ObservationAvailable() {
		t.Fatal("injected store inherited file notification")
	}
	var sources struct {
		sync.Mutex
		current chan struct{}
		started int
	}
	if e = h.EnableObservation(func(context.Context) (<-chan struct{}, func(), error) {
		sources.Lock()
		defer sources.Unlock()
		sources.current, sources.started = make(chan struct{}, 1), sources.started+1
		return sources.current, func() {}, nil
	}); e != nil {
		t.Fatal(e)
	}
	source := func() (chan struct{}, int) {
		sources.Lock()
		defer sources.Unlock()
		return sources.current, sources.started
	}
	done := make(chan error, 1)
	go func() { done <- h.Serve(context.Background()) }()
	defer func() { h.Close(); <-done }()
	c := client.NewObserver(endpoint)
	first, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0)
	if e != nil || first.Snapshot == nil || first.Snapshot.Store != "" {
		t.Fatal("wrong selected source", first, e)
	}
	editor := client.NewEditor(endpoint)
	u, e := editor.ReadUserContext(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	_, e = editor.ReplaceUserContext(context.Background(), u.Revision, wire.UserSettings{Store: "selected", Off: map[string]string{}})
	if e != nil {
		t.Fatal(e)
	}
	changes, _ := source()
	changes <- struct{}{}
	current, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, first.Cursor, 1000)
	if e != nil || current.Snapshot == nil || current.Snapshot.Store != "selected" {
		t.Fatal(current, e)
	}
	store.fail.Store(true)
	if _, e = c.ObserveContext(context.Background(), wire.RunOverrides{}, "", 0); e == nil {
		t.Fatal("unavailable selected store became a snapshot")
	}
	store.fail.Store(false)
	// A closed source is started again, and the new one delivers changes.
	close(changes)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if next, started := source(); started == 2 && h.ObservationAvailable() {
			changes = next
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed change source not started again")
		}
		runtime.Gosched()
	}
	if u, e = editor.ReadUserContext(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = editor.ReplaceUserContext(context.Background(), u.Revision, wire.UserSettings{Store: "again", Off: map[string]string{}}); e != nil {
		t.Fatal(e)
	}
	changes <- struct{}{}
	for cursor := current.Cursor; ; {
		next, e := c.ObserveContext(context.Background(), wire.RunOverrides{}, cursor, 1000)
		if e != nil || next.Snapshot == nil {
			t.Fatal(next, e)
		}
		if next.Snapshot.Store == "again" {
			break
		}
		cursor = next.Cursor
	}
}
