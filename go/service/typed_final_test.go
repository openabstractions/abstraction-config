package service

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"

	casapi "github.com/openabstractions/abstraction-cas/go/api"
	config "github.com/openabstractions/abstraction-config/go"
	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
	rights "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

type failingTypedRead struct {
	casapi.Store
	fail   atomic.Bool
	onRead func()
}

func (s *failingTypedRead) Read(path string) (casapi.Value, error) {
	if s.onRead != nil {
		s.onRead()
	}
	if s.fail.Load() {
		return casapi.Value{}, errors.New("inert read unavailable")
	}
	return s.Store.Read(path)
}

func TestTypedObservationGrantExpansionCannotPoisonNarrowCache(t *testing.T) {
	for _, restoreDuringRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "A-to-B", true: "A-to-B-to-A"}[restoreDuringRead], func(t *testing.T) {
			catalogue := wire.ConfigKeyDescriptors()
			var armed, expanded, afterStore atomic.Bool
			var rounds, checked atomic.Int32
			store := &failingTypedRead{Store: casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}}
			// The grant widens while storage reads: one pass decides both the
			// visibility check and the read's admission, so the widening can
			// land no earlier than the storage read.
			store.onRead = func() {
				if armed.Load() {
					expanded.Store(true)
					afterStore.Store(true)
				}
			}
			transport, _, path := typedTestHostStore(t, TypedOptions{ReadPolicy: func(_ context.Context, _ *identity.Peer, key string) error {
				if armed.Load() && key == catalogue[0].Key && rounds.Add(1) == 3 {
					expanded.Store(true)
				}
				allow := expanded.Load() || key == "nas_store"
				if armed.Load() && afterStore.Load() && expanded.Load() && checked.Add(1) == int32(2*len(catalogue)) && restoreDuringRead {
					expanded.Store(false)
				}
				if !allow {
					return errors.New("narrow grant")
				}
				return nil
			}}, func(string) casapi.Store { return store })
			if err := os.WriteFile(path, []byte(`{"nas_store":"narrow","messages_start_notes":true,"logs_max_use":64,"private_future_name":1}`), 0600); err != nil {
				t.Fatal(err)
			}
			client := wire.NewConfigObserverV2Client(transport)
			first, err := client.Observe("", 0)
			if err != nil || first.Snapshot == nil || len(first.Snapshot.Entries) != 1 {
				t.Fatal(first, err)
			}
			armed.Store(true)
			attack, err := client.Observe("", 0)
			if err != nil || attack.Outcome != wire.ObservationOutcomeV2Gap || attack.Snapshot != nil {
				t.Fatal("expanded read escaped initial visibility", attack, err)
			}
			if checked.Load() < int32(2*len(catalogue)) {
				t.Fatal("fixture did not admit and recheck broad snapshot", checked.Load())
			}
			armed.Store(false)
			expanded.Store(false)
			store.fail.Store(true)
			unavailable, err := client.Observe("", 0)
			if err != nil || unavailable.Outcome != wire.ObservationOutcomeV2Unavailable || unavailable.Snapshot == nil || unavailable.AgeMs == nil {
				t.Fatal(unavailable, err)
			}
			if len(unavailable.Snapshot.Entries) != 1 || unavailable.Snapshot.Entries[0].Key != "nas_store" || len(unavailable.Snapshot.IgnoredFields) != 0 || unavailable.Snapshot.IgnoredOmitted != 0 {
				t.Fatal("broader cached contents leaked", unavailable)
			}
		})
	}
}

func TestTypedObservationRetainsAuthorizedSnapshotAndAge(t *testing.T) {
	var revoked atomic.Bool
	store := &failingTypedRead{Store: casapi.BoundedFileStore{MaxBytes: config.MaxUserFileBytes}}
	transport, _, path := typedTestHostStore(t, TypedOptions{ReadPolicy: func(_ context.Context, _ *identity.Peer, key string) error {
		selected := "nas_store"
		if revoked.Load() {
			selected = "messages_start_notes"
		}
		if key != selected {
			return errors.New("no grant")
		}
		return nil
	}}, func(string) casapi.Store { return store })
	if err := os.WriteFile(path, []byte(`{"nas_store":"inert-share"}`), 0600); err != nil {
		t.Fatal(err)
	}
	client := wire.NewConfigObserverV2Client(transport)
	good, err := client.Observe("", 0)
	if err != nil || good.Outcome != wire.ObservationOutcomeV2Changed || good.Snapshot == nil || len(good.Snapshot.Entries) != 1 {
		t.Fatal(good, err)
	}
	store.fail.Store(true)
	failed, err := client.Observe(good.Cursor, 0)
	if err != nil || failed.Outcome != wire.ObservationOutcomeV2Unavailable || failed.Snapshot == nil || failed.AgeMs == nil || *failed.AgeMs < 0 || failed.Snapshot.Entries[0].Value.Text != "inert-share" {
		t.Fatal(failed, err)
	}
	revoked.Store(true)
	refused, err := client.Observe(failed.Cursor, 0)
	if err != nil || refused.Outcome != wire.ObservationOutcomeV2Gap || refused.Snapshot != nil || refused.AgeMs != nil {
		t.Fatal(refused, err)
	}
	current, err := client.Observe("", 0)
	if err != nil || current.Outcome != wire.ObservationOutcomeV2Unavailable || current.Snapshot != nil || current.AgeMs != nil {
		t.Fatal(current, err)
	}
}

func TestTypedDescribeAndRefusalDetail(t *testing.T) {
	transport, host, _ := typedTestHost(t, TypedOptions{})
	client := wire.NewConfigReaderV2Client(transport)
	description, err := client.Describe()
	if err != nil || description.Outcome != wire.ReadOutcomeRead || len(description.Descriptors) == 0 || len(description.Descriptors) > 64 || description.Refusal != nil {
		t.Fatal(description, err)
	}
	result, err := client.Read([]string{"nas_store", "unknown"})
	if err != nil || len(result.Refused) != 2 || result.Refused[0].Refusal == nil || result.Refused[0].Refusal.Source != rights.RefusalSourceRights || result.Refused[0].Refusal.Decision != rights.RefusalDecisionNotGranted || result.Refused[0].Refusal.Ask != rights.RefusalAskNone || result.Refused[1].Refusal != nil {
		t.Fatal(result, err)
	}
	empty, err := wire.NewConfigEditorV2Client(transport).ReadUser()
	if err != nil || empty.Snapshot == nil {
		t.Fatal(empty, err)
	}
	for _, entry := range empty.Snapshot.Entries {
		if entry.Origin.State == wire.KeyStateDefault && (entry.Origin.Location != 0 || entry.Origin.Path != "") {
			t.Fatal("default has a location", entry)
		}
	}
	// An owner mismatch exercises the same-account boundary before any read.
	host.owner = "inert-other-account"
	boundary, err := client.Describe()
	if err != nil || boundary.Outcome != wire.ReadOutcomeForbidden || len(boundary.Descriptors) != 0 || boundary.Refusal == nil || boundary.Refusal.Source != rights.RefusalSourceIdentity {
		t.Fatal(boundary, err)
	}
	read, err := client.Read(nil)
	if err != nil || read.Outcome != wire.ReadOutcomeForbidden || read.Refusal == nil || len(read.Entries) != 0 {
		t.Fatal(read, err)
	}
	user, err := wire.NewConfigEditorV2Client(transport).ReadUser()
	if err != nil || user.Outcome != wire.ReadOutcomeForbidden || user.Refusal == nil || user.Snapshot != nil {
		t.Fatal(user, err)
	}
}

func TestTypedObservationNotificationFailureEndsWithCause(t *testing.T) {
	transport, host, _ := typedTestHost(t, TypedOptions{})
	client := wire.NewConfigObserverV2Client(transport)
	first, err := client.Observe("", 0)
	if err != nil || first.Outcome != wire.ObservationOutcomeV2Changed {
		t.Fatal(first, err)
	}
	host.observation.gate.Fail(errors.New("inert notification failure"))
	ended, err := client.Observe(first.Cursor, 0)
	if err != nil || ended.Outcome != wire.ObservationOutcomeV2Failed || ended.Cause != "notification_source_failed" || ended.Snapshot != nil || ended.Cursor != first.Cursor {
		t.Fatal(ended, err)
	}
}

func TestTypedCallOutcomesRefuseUnknownWords(t *testing.T) {
	for _, example := range []struct {
		target any
		data   string
	}{
		{&wire.ReadResult{}, `{"outcome":"future","entries":[],"revision":""}`},
		{&wire.UserReplaceResultV2{}, `{"outcome":"future","snapshot":{"entries":[],"revision":""}}`},
		{&wire.ConfigObservationV2{}, `{"outcome":"future","cursor":""}`},
	} {
		var refusal *wire.Refusal
		if err := wire.FromJSON([]byte(example.data), example.target); !errors.As(err, &refusal) || refusal.Word != "bad_enum" {
			t.Fatalf("unknown call outcome not refused as an enum: %T: %v", example.target, err)
		}
	}
}
