package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
	watchcursor "github.com/openabstractions/abstraction-watch/go/cursor"
)

func TestTypedObservationCursorEnvelopeCompatibility(t *testing.T) {
	var alternate atomic.Bool
	policy := func(_ context.Context, _ *identity.Peer, key string) error {
		selected := "nas_store"
		if alternate.Load() {
			selected = "messages_start_notes"
		}
		if key == selected {
			return nil
		}
		return errors.New("fixture key not granted")
	}
	transport, host, path := typedTestHost(t, TypedOptions{ReadPolicy: policy})
	client := wire.NewConfigObserverV2Client(transport)
	first, err := client.Observe("", 0)
	if err != nil || first.Outcome != wire.ObservationOutcomeV2Changed || first.Snapshot == nil {
		t.Fatal(first, err)
	}
	codec := watchcursor.Codec{Separator: ":", Scoped: true}
	envelope, ok := codec.Parse(first.Cursor)
	if !ok || !envelope.Hex(16, 32, 32) || codec.Encode(envelope) != first.Cursor {
		t.Fatal("cursor representation changed", first.Cursor)
	}
	data, err := wire.ToJSON(*first.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	if envelope.Payload != hex.EncodeToString(digest[:]) {
		t.Fatal("cursor lost effective snapshot digest")
	}
	check := func(cursor string, want wire.ObservationOutcomeV2) wire.ConfigObservationV2 {
		t.Helper()
		answer, err := client.Observe(cursor, 0)
		if err != nil || answer.Outcome != want {
			t.Fatal(cursor, answer, err)
		}
		if want != wire.ObservationOutcomeV2Changed && (answer.Cursor != cursor || answer.Snapshot != nil || answer.AgeMs != nil) {
			t.Fatal("refusal disclosed or changed state", answer)
		}
		return answer
	}
	check(first.Cursor, wire.ObservationOutcomeV2Unchanged)
	for _, malformed := range []string{
		"bad", envelope.Epoch + ":" + envelope.Scope, first.Cursor + ":tail",
		"g" + first.Cursor[1:], envelope.Epoch + ":g" + envelope.Scope[1:] + ":" + envelope.Payload,
		envelope.Epoch + ":" + envelope.Scope + ":g" + envelope.Payload[1:],
		envelope.Epoch[1:] + ":" + envelope.Scope + ":" + envelope.Payload,
		strings.Repeat("a", 512),
	} {
		check(malformed, wire.ObservationOutcomeV2Invalid)
	}
	flip := func(value string) string {
		if value[0] == '0' {
			return "1" + value[1:]
		}
		return "0" + value[1:]
	}
	foreign := envelope
	foreign.Epoch = flip(foreign.Epoch)
	check(codec.Encode(foreign), wire.ObservationOutcomeV2Gap)
	foreign = envelope
	foreign.Scope = flip(foreign.Scope)
	check(codec.Encode(foreign), wire.ObservationOutcomeV2Gap)
	stale := envelope
	stale.Payload = flip(stale.Payload)
	if current := check(codec.Encode(stale), wire.ObservationOutcomeV2Changed); current.Cursor != first.Cursor || current.Snapshot == nil {
		t.Fatal(current)
	}
	if err := os.WriteFile(path, []byte(`{"nas_store":"cursor-fixture"}`), 0600); err != nil {
		t.Fatal(err)
	}
	changed := check(first.Cursor, wire.ObservationOutcomeV2Changed)
	if changed.Cursor == first.Cursor || changed.Snapshot == nil {
		t.Fatal("stale revision was not refreshed", changed)
	}
	alternate.Store(true)
	check(changed.Cursor, wire.ObservationOutcomeV2Gap)
	if reset := check("", wire.ObservationOutcomeV2Changed); reset.Snapshot == nil {
		t.Fatal(reset)
	}
	owner := host.owner
	host.owner = "fixture-other-owner"
	if denied := check("malformed", wire.ObservationOutcomeV2Forbidden); denied.Refusal == nil {
		t.Fatal("authorization did not precede cursor parsing", denied)
	}
	host.owner = owner
	alternate.Store(false)
	host.Close()
	freshTransport, _, _ := typedTestHost(t, TypedOptions{ReadPolicy: policy})
	fresh := wire.NewConfigObserverV2Client(freshTransport)
	reset, err := fresh.Observe("", 0)
	if err != nil || reset.Outcome != wire.ObservationOutcomeV2Changed {
		t.Fatal(reset, err)
	}
	newEnvelope, ok := codec.Parse(reset.Cursor)
	if !ok || newEnvelope.Epoch == envelope.Epoch || newEnvelope.Scope != envelope.Scope {
		t.Fatal("restart control changed scope or retained epoch", newEnvelope)
	}
	restarted, err := fresh.Observe(changed.Cursor, 0)
	if err != nil || restarted.Outcome != wire.ObservationOutcomeV2Gap || restarted.Snapshot != nil {
		t.Fatal("provider restart accepted old epoch", restarted, err)
	}
}
