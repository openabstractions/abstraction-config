package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
)

func TestUserEditorPreservesFutureFields(t *testing.T) {
	requireProgramProof(t)
	isolated(t)
	path := config.UserPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"future":true,"STORE":"case","messages_start_notes":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	editor, _ := editorHost(t, false)
	before, err := editor.ReadUser()
	if err != nil {
		t.Fatal(err)
	}
	after, err := editor.ReplaceUser(before.Revision, wire.UserSettings{NasStore: "replacement"})
	if err != nil || after.Outcome != wire.UserReplaceOutcomeApplied {
		t.Fatal(after, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["future"]) != "true" || string(fields["STORE"]) != `"case"` || string(fields["messages_start_notes"]) != "true" {
		t.Fatal("legacy write dropped future fields", string(data))
	}
}

func typedTestHost(t *testing.T, options TypedOptions) (listen.FrameClient, *Host, string) {
	return typedTestHostStore(t, options, nil)
}
func typedTestHostStore(t *testing.T, options TypedOptions, selectStore func(string) casapi.Store) (listen.FrameClient, *Host, string) {
	t.Helper()
	requireProgramProof(t)
	path := filepath.Join(t.TempDir(), "config.json")
	endpoint := listen.Endpoint(fmt.Sprintf("config-typed-%d-%d", os.Getpid(), serial.Add(1)))
	var store casapi.Store = casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}
	if selectStore != nil {
		store = selectStore(path)
	}
	h, err := ListenWithStore(endpoint, store, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.OmitMachineRung(); err != nil {
		t.Fatal(err)
	}
	if err := h.EnableTypedConfig(options); err != nil {
		t.Fatal(err)
	}
	if err := h.EnableObservation(FileObservation(path)); err != nil {
		t.Fatal(err)
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
		case <-time.After(2 * time.Second):
			t.Error("typed host did not stop")
		}
	})
	return listen.FrameClient{Endpoint: endpoint, Timeout: 2 * time.Second, MaxFrame: 1 << 20}, h, path
}

type pausedConfigRead struct {
	casapi.Store
	entered, release chan struct{}
	once             sync.Once
}

func (s *pausedConfigRead) Read(path string) (casapi.Value, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.Store.Read(path)
}

func TestTypedReadRevocationWhileStorageWaitsDisclosesNothing(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	defer unblock.Do(func() { close(release) })
	var revoked atomic.Bool
	transport, _, path := typedTestHostStore(t, TypedOptions{ReadPolicy: func(_ context.Context, peer *identity.Peer, key string) error {
		if peer == nil || revoked.Load() {
			return errors.New("revoked")
		}
		return nil
	}}, func(string) casapi.Store {
		return &pausedConfigRead{Store: casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}, entered: entered, release: release}
	})
	if err := os.WriteFile(path, []byte(`{"messages_start_notes":true,"future_field":{"opaque":"kept"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	type response struct {
		result wire.ReadResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, err := wire.NewConfigReaderV2Client(transport).Read([]string{"messages_start_notes"})
		done <- response{result, err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("read did not reach inert store")
	}
	revoked.Store(true)
	unblock.Do(func() { close(release) })
	select {
	case answer := <-done:
		if answer.err != nil || answer.result.Outcome != wire.ReadOutcomeRead || len(answer.result.Entries) != 0 || len(answer.result.IgnoredFields) != 0 || len(answer.result.Refused) != 1 || answer.result.Refused[0].Reason != wire.KeyRefusalReasonNotGranted {
			t.Fatal(answer)
		}
	case <-time.After(time.Second):
		t.Fatal("revoked read did not finish")
	}
}

func TestTypedReadNilProviderProofRefusesBeforeStorage(t *testing.T) {
	transport, _, path := typedTestHost(t, TypedOptions{})
	// Deliberately corrupt selected fixture proves nil cannot reach storage.
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := wire.NewConfigReaderV2Client(transport).Read([]string{"messages_start_questions", "missing"})
	if err != nil || result.Outcome != wire.ReadOutcomeRead || len(result.Entries) != 0 || len(result.Refused) != 2 {
		t.Fatal(result, err)
	}
	if result.Refused[0].Reason != wire.KeyRefusalReasonNotGranted || result.Refused[1].Reason != wire.KeyRefusalReasonUnknownKey {
		t.Fatal(result.Refused)
	}
}

func TestTypedProviderExactKeysRecheckedOnEveryRead(t *testing.T) {
	var calls atomic.Int32
	var revoked atomic.Bool
	transport, _, _ := typedTestHost(t, TypedOptions{ReadPolicy: func(ctx context.Context, peer *identity.Peer, key string) error {
		calls.Add(1)
		if peer == nil || ctx.Err() != nil || revoked.Load() || key != "messages_start_questions" {
			return errors.New("not registered for key")
		}
		return nil
	}})
	client := wire.NewConfigReaderV2Client(transport)
	first, err := client.Read([]string{"messages_start_questions", "messages_start_notes"})
	if err != nil || len(first.Entries) != 1 || !first.Entries[0].Value.Boolean || len(first.Refused) != 1 {
		t.Fatal(first, err)
	}
	revoked.Store(true)
	beforeRevokedRead := calls.Load()
	second, err := client.Read([]string{"messages_start_questions"})
	if err != nil || len(second.Entries) != 0 || len(second.Refused) != 1 || calls.Load() <= beforeRevokedRead {
		t.Fatal(second, calls.Load(), err)
	}
}

func TestTypedEditorAtomicUnsetAndStoredCorruption(t *testing.T) {
	transport, _, path := typedTestHost(t, TypedOptions{})
	editor := wire.NewConfigEditorV2Client(transport)
	initial, err := editor.ReadUser()
	if err != nil || initial.Snapshot == nil {
		t.Fatal(initial, err)
	}
	changed, err := editor.ReplaceUser(initial.Snapshot.Revision, []wire.Change{{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean, Boolean: true}}})
	if err != nil || changed.Outcome != wire.ReplaceOutcomeApplied {
		t.Fatal(changed, err)
	}
	beforeInvalid, _ := os.ReadFile(path)
	invalid, err := editor.ReplaceUser(changed.Snapshot.Revision, []wire.Change{{Key: "messages_start_notes"}, {Key: "messages_start_per_hour", Set: &wire.Value{Kind: wire.ValueKindChoice, Choice: "3"}}})
	if err != nil || invalid.Outcome != wire.ReplaceOutcomeInvalid || len(invalid.Refused) != 1 {
		t.Fatal(invalid, err)
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, beforeInvalid) {
		t.Fatal("invalid batch partially unset")
	}
	if err := os.WriteFile(path, []byte(`{"messages_start_notes":"invalid"}`), 0600); err != nil {
		t.Fatal(err)
	}
	ignored, err := editor.ReadUser()
	if err != nil || ignored.Outcome != wire.ReadOutcomeRead || ignored.Snapshot == nil {
		t.Fatal(ignored, err)
	}
	found := false
	for _, entry := range ignored.Snapshot.Entries {
		if entry.Key == "messages_start_notes" {
			found = entry.Origin.State == wire.KeyState("ignored") && entry.Origin.Reason == "invalid_value"
		}
	}
	if !found {
		t.Fatal("missing ignored saved value", ignored)
	}
	broken := `{"messages_start_notes":!}`
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt, err := editor.ReadUser()
	if err != nil || corrupt.Outcome != wire.ReadOutcomeCorrupt || corrupt.Snapshot != nil || corrupt.CorruptOffset != int64(strings.IndexByte(broken, '!')) {
		t.Fatal(corrupt, err)
	}
}

func TestTypedObserverPendingAndCorruptionThroughNativeInvalidation(t *testing.T) {
	transport, _, path := typedTestHost(t, TypedOptions{
		ReadPolicy: func(_ context.Context, _ *identity.Peer, key string) error {
			if key == "nas_store" {
				return nil
			}
			return errors.New("not designated")
		},
		RestartValues: map[string]*wire.Value{"nas_store": {Kind: wire.ValueKindText, Text: "old"}},
	})
	if err := os.WriteFile(path, []byte(`{"nas_store":"old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	observer := wire.NewConfigObserverV2Client(transport)
	first, err := observer.Observe("", 0)
	if err != nil || first.Outcome != wire.ObservationOutcomeV2Changed || first.Snapshot == nil || len(first.Snapshot.Entries) != 1 {
		t.Fatal(first, err)
	}
	if first.Snapshot.Entries[0].Origin.State != wire.KeyState("set") {
		t.Fatal(first.Snapshot.Entries)
	}
	editor := wire.NewConfigEditorV2Client(transport)
	user, err := editor.ReadUser()
	if err != nil || user.Snapshot == nil {
		t.Fatal(user, err)
	}
	changed, err := editor.ReplaceUser(user.Snapshot.Revision, []wire.Change{{Key: "nas_store", Set: &wire.Value{Kind: wire.ValueKindText, Text: "new"}}})
	if err != nil || changed.Outcome != wire.ReplaceOutcomeApplied {
		t.Fatal(changed, err)
	}
	second, err := observer.Observe(first.Cursor, 1000)
	if err != nil || second.Outcome != wire.ObservationOutcomeV2Changed || second.Cursor == first.Cursor || second.Snapshot == nil {
		t.Fatal(second, err)
	}
	if entry := second.Snapshot.Entries[0]; entry.Value.Text != "new" || entry.Origin.State != wire.KeyState("pending") {
		t.Fatal(entry)
	}
	unchanged, err := observer.Observe(second.Cursor, 0)
	if err != nil || unchanged.Outcome != wire.ObservationOutcomeV2Unchanged || unchanged.Snapshot != nil {
		t.Fatal(unchanged, err)
	}
	if err := os.WriteFile(path, []byte(`{"nas_store":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	ignored, err := observer.Observe(second.Cursor, 1000)
	if err != nil || ignored.Outcome != wire.ObservationOutcomeV2Changed || ignored.Snapshot == nil || ignored.Snapshot.Entries[0].Origin.State != wire.KeyState("ignored") || ignored.Snapshot.Entries[0].Origin.Reason != "invalid_value" {
		t.Fatal(ignored, err)
	}
	broken := `{"nas_store":!}`
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	corrupt, err := observer.Observe(ignored.Cursor, 1000)
	if err != nil || corrupt.Outcome != wire.ObservationOutcomeV2Corrupt || corrupt.Snapshot != nil || corrupt.CorruptOffset != int64(strings.IndexByte(broken, '!')) {
		t.Fatal(corrupt, err)
	}
}
