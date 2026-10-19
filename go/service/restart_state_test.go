package service

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
)

func TestTypedRestartAdmissionInvalidatesObserverWithoutFileEdit(t *testing.T) {
	state := new(RestartState)
	transport, host, path := typedTestHost(t, TypedOptions{RestartState: state,
		ReadPolicy: func(_ context.Context, _ *identity.Peer, key string) error {
			if key == "nas_store" {
				return nil
			}
			return errors.New("not granted")
		}})
	if err := os.WriteFile(path, []byte(`{"nas_store":"configured"}`), 0600); err != nil {
		t.Fatal(err)
	}
	observer := wire.NewConfigObserverV2Client(transport)
	first, err := observer.Observe("", 0)
	if err != nil || first.Snapshot == nil {
		t.Fatal(first, err)
	}
	answer := make(chan wire.ConfigObservationV2, 1)
	go func() {
		value, err := observer.Observe(first.Cursor, 2000)
		if err != nil {
			t.Error(err)
		}
		answer <- value
	}()
	waitConfigObservers(t, host, 1)
	admitted := &wire.Value{Kind: wire.ValueKindText, Text: "actual-bound-store"}
	if err := state.Admit("nas_store", admitted); err != nil {
		t.Fatal(err)
	}
	admitted.Text = "caller mutation"
	select {
	case next := <-answer:
		if next.Snapshot == nil || next.Cursor == first.Cursor || next.Snapshot.Entries[0].Origin.State != wire.KeyState("pending") {
			t.Fatal(next)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admission did not invalidate observer")
	}
	values, _ := state.snapshot()
	if values["nas_store"].Text != "actual-bound-store" {
		t.Fatal(values)
	}
	values["nas_store"].Text = "reader mutation"
	values, _ = state.snapshot()
	if values["nas_store"].Text != "actual-bound-store" {
		t.Fatal(values)
	}
	if err := state.Admit("messages_start_notes", &wire.Value{Kind: wire.ValueKindBoolean}); err == nil {
		t.Fatal("live key admitted as restart state")
	}
}

func TestTypedScopedReadHidesUnknownDiagnosticsAndRevisionChanges(t *testing.T) {
	transport, _, path := typedTestHost(t, TypedOptions{ReadPolicy: func(_ context.Context, _ *identity.Peer, key string) error {
		if key == "messages_start_questions" {
			return nil
		}
		return errors.New("not granted")
	}})
	reader := wire.NewConfigReaderV2Client(transport)
	if err := os.WriteFile(path, []byte(`{"messages_start_questions":true,"secret_future_name":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := reader.Read([]string{"messages_start_questions"})
	if err != nil || first.Outcome != wire.ReadOutcomeRead || len(first.Entries) != 1 {
		t.Fatal(first, err)
	}
	if err := os.WriteFile(path, []byte(`{"messages_start_questions":true,"different_future_name":{"changed":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := reader.Read([]string{"messages_start_questions"})
	if err != nil || first.Revision != second.Revision || len(second.IgnoredFields) != 0 || second.IgnoredOmitted != 0 {
		t.Fatal(first, second, err)
	}
}

func TestTypedResultProducersQualifyCorruptOffset(t *testing.T) {
	transport, _, path := typedTestHost(t, TypedOptions{ReadPolicy: func(context.Context, *identity.Peer, string) error { return nil }, DynamicDefault: func(string) (*wire.Value, error) { return &wire.Value{Kind: wire.ValueKindBytes, Bytes: 1024}, nil }})
	reader := wire.NewConfigReaderV2Client(transport)
	editor := wire.NewConfigEditorV2Client(transport)
	observer := wire.NewConfigObserverV2Client(transport)
	if err := os.WriteFile(path, []byte(`{"nas_store":"ok","future":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := editor.ReadUser()
	if err != nil || before.Snapshot == nil {
		t.Fatal(before, err)
	}
	if err := os.WriteFile(path, []byte("  !"), 0600); err != nil {
		t.Fatal(err)
	}
	read, err := reader.Read([]string{"nas_store"})
	if err != nil || read.Outcome != wire.ReadOutcomeCorrupt || read.CorruptOffset != 2 || read.CorruptSource == nil || read.CorruptSource.Location != wire.LocationUser || read.CorruptSource.Path != path {
		t.Fatal(read, err)
	}
	user, err := editor.ReadUser()
	if err != nil || user.Outcome != wire.ReadOutcomeCorrupt || user.CorruptOffset != 2 {
		t.Fatal(user, err)
	}
	replaced, err := editor.ReplaceUser(before.Snapshot.Revision, []wire.Change{{Key: "nas_store"}})
	if err != nil || replaced.Outcome != wire.ReplaceOutcomeCorrupt || replaced.CorruptOffset != 2 {
		t.Fatal(replaced, err)
	}
	observed, err := observer.Observe("", 0)
	if err != nil || observed.Outcome != wire.ObservationOutcomeV2Corrupt || observed.CorruptOffset != 2 || observed.CorruptSource == nil || observed.CorruptSource.Path != path || observed.CorruptSource.Location != wire.LocationUser {
		t.Fatal(observed, err)
	}
	if err := os.WriteFile(path, []byte(`{"nas_store":"ok","future":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	read, err = reader.Read([]string{"nas_store"})
	if err != nil || read.Outcome != wire.ReadOutcomeRead || read.CorruptOffset != 0 || read.CorruptSource != nil || len(read.IgnoredFields) != 1 || read.IgnoredFields[0].Source.Path != path || read.IgnoredFields[0].Source.Location != wire.LocationUser {
		t.Fatal(read, err)
	}
	user, err = editor.ReadUser()
	if err != nil || user.Outcome != wire.ReadOutcomeRead || user.CorruptOffset != 0 {
		t.Fatal(user, err)
	}
	replaced, err = editor.ReplaceUser(user.Snapshot.Revision, []wire.Change{{Key: "nas_store"}})
	if err != nil || replaced.Outcome != wire.ReplaceOutcomeApplied || replaced.CorruptOffset != 0 {
		t.Fatal(replaced, err)
	}
	observed, err = observer.Observe("", 0)
	if err != nil || observed.Outcome != wire.ObservationOutcomeV2Changed || observed.CorruptOffset != 0 || observed.CorruptSource != nil {
		t.Fatal(observed, err)
	}
}

func TestTypedObserverForwardsBothCorruptSourcesOnlyUnderCorrupt(t *testing.T) {
	for _, location := range []wire.Location{wire.LocationMachine, wire.LocationUser} {
		source := &wire.ConfigSource{Location: location, Path: "selected-source"}
		for _, outcome := range []string{"corrupt", "unavailable", "invalid", "not_permitted"} {
			word, _ := wire.ParseReadOutcome(outcome)
			answer := typedObservationRefusal(wire.ReadResult{Outcome: word, CorruptOffset: 17, CorruptSource: source}, "cursor")
			if outcome == "corrupt" {
				if answer.CorruptOffset != 17 || answer.CorruptSource != source {
					t.Fatal(answer)
				}
			} else if answer.CorruptOffset != 0 || answer.CorruptSource != nil {
				t.Fatal(answer)
			}
		}
	}
}

func TestTypedObserverWaitsForRepairOfUnchangedCorruption(t *testing.T) {
	transport, host, path := typedTestHost(t, TypedOptions{ReadPolicy: func(_ context.Context, _ *identity.Peer, key string) error {
		if key == "nas_store" {
			return nil
		}
		return errors.New("not granted")
	}})
	if err := os.WriteFile(path, []byte("!"), 0600); err != nil {
		t.Fatal(err)
	}
	observer := wire.NewConfigObserverV2Client(transport)
	first, err := observer.Observe("", 0)
	if err != nil || first.Outcome != wire.ObservationOutcomeV2Corrupt || first.Cursor == "" {
		t.Fatal(first, err)
	}
	unchanged, err := observer.Observe(first.Cursor, 0)
	if err != nil || unchanged.Outcome != wire.ObservationOutcomeV2Unchanged || unchanged.CorruptSource != nil {
		t.Fatal(unchanged, err)
	}
	answer := make(chan wire.ConfigObservationV2, 1)
	go func() {
		value, err := observer.Observe(first.Cursor, 2000)
		if err != nil {
			t.Error(err)
		}
		answer <- value
	}()
	waitConfigObservers(t, host, 1)
	if err := os.WriteFile(path, []byte(`{"nas_store":"repaired"}`), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case next := <-answer:
		if next.Outcome != wire.ObservationOutcomeV2Changed || next.Snapshot == nil || next.Snapshot.Entries[0].Value.Text != "repaired" {
			t.Fatal(next)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("repair did not invalidate corrupt cursor")
	}
}
