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
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var serial atomic.Uint64

func isolated(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, key := range []string{"APPDATA", "HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(key, home)
	}
	t.Setenv("ProgramData", filepath.Join(home, "machine"))
	for _, key := range []string{"ABSTRACTION_NAS_STORE", "ABSTRACTION_STORE", "ABSTRACTION_LOG", "ABSTRACTION_LOG_SERVICE"} {
		t.Setenv(key, "")
	}
}
func host(t *testing.T, configure ...func(*Host)) (*Host, *client.Client) {
	t.Helper()
	endpoint := listen.Endpoint(fmt.Sprintf("config-test-%d-%d", os.Getpid(), serial.Add(1)))
	h, err := Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range configure {
		f(h)
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
			t.Error("host did not stop")
		}
	})
	return h, client.New(endpoint)
}
func TestReadExistingProviderAndExplicitRun(t *testing.T) {
	isolated(t)
	path := config.UserPath()
	want := config.Config{Store: "user-store", NASStore: "user-nas", LogSink: "user-log", LogService: "user-service", Off: map[string]string{"nas": "maintenance"}}
	if err := config.Save(path, want); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ABSTRACTION_STORE", "host-run-must-not-leak")
	_, c := host(t)
	snapshot, err := c.ReadWithOverrides(wire.RunOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Store != "user-store" || snapshot.Origins.Store.Rung != config.User || snapshot.Origins.Store.Path != path || snapshot.Off["nas"] != "maintenance" || snapshot.Stamp != want.Stamp() {
		t.Fatalf("wrong provider answer: %+v", snapshot)
	}
	overridden, err := c.ReadWithOverrides(wire.RunOverrides{Store: "caller-run"})
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Store != "caller-run" || overridden.Origins.Store.Rung != config.Environment || overridden.Origins.Store.Path != "" || overridden.Origins.NasStore.Rung != config.User {
		t.Fatalf("wrong override provenance: %+v", overridden)
	}
	same, err := c.ReadWithOverrides(wire.RunOverrides{Store: "user-store"})
	if err != nil || same.Stamp != snapshot.Stamp || same.Origins.Store.Rung != config.Environment {
		t.Fatalf("stamp followed provenance: %+v %v", same, err)
	}
	want.Store = "changed-by-provider"
	if err := config.Save(path, want); err != nil {
		t.Fatal(err)
	}
	changed, err := c.ReadWithOverrides(wire.RunOverrides{})
	if err != nil || changed.Store != want.Store {
		t.Fatalf("stale client/provider answer: %+v %v", changed, err)
	}
}
func TestWrongUserRefusedBeforeProvider(t *testing.T) {
	isolated(t)
	var reads atomic.Int32
	_, c := host(t, func(h *Host) {
		h.owner = "not-the-bound-principal"
		h.load = func(map[string]string) config.Config { reads.Add(1); return config.Config{} }
	})
	_, err := c.ReadWithOverrides(wire.RunOverrides{})
	var refusal *wire.ServiceError
	if !errors.As(err, &refusal) || refusal.Code != "wrong_user" || reads.Load() != 0 {
		t.Fatalf("refusal=%v provider reads=%d", err, reads.Load())
	}
}

type addingUnknown struct{ inner listen.FrameClient }

func (a addingUnknown) ExchangeFrame(frame []byte) ([]byte, error) {
	text := strings.Replace(string(frame), `"overrides": {`, `"overrides": {"unexpected": true,`, 1)
	if text == string(frame) {
		return nil, errors.New("fixture could not insert unknown field")
	}
	return a.inner.ExchangeFrame([]byte(text))
}
func TestUnknownFieldRefusedBeforeProvider(t *testing.T) {
	isolated(t)
	var reads atomic.Int32
	endpoint := listen.Endpoint(fmt.Sprintf("config-refusal-%d-%d", os.Getpid(), serial.Add(1)))
	other, err := Listen(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	other.load = func(map[string]string) config.Config { reads.Add(1); return config.Config{} }
	defer other.Close()
	done := make(chan error, 1)
	go func() { done <- other.Serve(context.Background()) }()
	transport := addingUnknown{listen.FrameClient{Endpoint: endpoint, Timeout: time.Second}}
	_, err = wire.NewConfigReaderClient(transport).Read(wire.RunOverrides{})
	var refusal *wire.ServiceError
	if !errors.As(err, &refusal) || refusal.Code != "unknown_field" || reads.Load() != 0 {
		t.Fatalf("refusal=%v reads=%d", err, reads.Load())
	}
	other.Close()
	<-done
}
func TestAbsentDoesNotFallback(t *testing.T) {
	isolated(t)
	c := client.New(listen.Endpoint(fmt.Sprintf("config-absent-%d-%d", os.Getpid(), serial.Add(1))))
	_, err := c.Read()
	if err == nil {
		t.Fatal("absent service silently returned local configuration")
	}
}
func TestMalformedSourceUsesProviderFallback(t *testing.T) {
	isolated(t)
	path := config.UserPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0600); err != nil {
		t.Fatal(err)
	}
	_, c := host(t)
	got, err := c.ReadWithOverrides(wire.RunOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	expected := config.LoadWithOverrides(nil)
	if got.Stamp != expected.Stamp() || got.Origins.Store.Rung != expected.Origin("store").Rung {
		t.Fatalf("provider fallback changed: %+v", got)
	}
}
