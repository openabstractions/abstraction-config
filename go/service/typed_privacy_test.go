package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
)

func TestTypedReplaceSnapshotOnlyOnAppliedOrConflict(t *testing.T) {
	for _, refusal := range []string{"forbidden", "not_permitted", "invalid", "unavailable"} {
		t.Run(refusal, func(t *testing.T) {
			transport, host, path := typedTestHost(t, TypedOptions{})
			stored := []byte(`{"nas_store":"private-user-state"}`)
			if err := os.WriteFile(path, stored, 0600); err != nil {
				t.Fatal(err)
			}
			editor := wire.NewConfigEditorV2Client(transport)
			before, err := editor.ReadUser()
			if err != nil || before.Snapshot == nil {
				t.Fatal(before, err)
			}
			change := []wire.Change{{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean, Boolean: true}}}
			if refusal == "forbidden" {
				host.owner = "other-account"
			}
			if refusal == "not_permitted" || refusal == "unavailable" {
				host.editPolicy = func(context.Context, *identity.Peer) error {
					if refusal == "unavailable" {
						return ErrEditPolicyUnavailable
					}
					return errors.New("inert denied edit")
				}
			}
			if refusal == "invalid" {
				change[0].Key = "unknown-setting"
			}
			result, err := editor.ReplaceUser(before.Snapshot.Revision, change)
			if err != nil || result.Outcome.String() != refusal || result.Snapshot != nil {
				t.Fatal(result, err)
			}
			encoded := wire.EncodeUserReplaceResultV2(&result)
			for _, secret := range [][]byte{[]byte(`"snapshot"`), []byte("private-user-state"), []byte(before.Snapshot.Revision)} {
				if bytes.Contains(encoded, secret) {
					t.Fatal("refused edit disclosed user state", string(encoded))
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, stored) {
				t.Fatal("refused edit changed state", string(after), err)
			}
		})
	}
	transport, _, _ := typedTestHost(t, TypedOptions{})
	editor := wire.NewConfigEditorV2Client(transport)
	before, err := editor.ReadUser()
	if err != nil || before.Snapshot == nil {
		t.Fatal(before, err)
	}
	change := []wire.Change{{Key: "messages_start_notes", Set: &wire.Value{Kind: wire.ValueKindBoolean, Boolean: true}}}
	applied, err := editor.ReplaceUser(before.Snapshot.Revision, change)
	if err != nil || applied.Outcome != wire.ReplaceOutcomeApplied || applied.Snapshot == nil {
		t.Fatal(applied, err)
	}
	conflict, err := editor.ReplaceUser(before.Snapshot.Revision, change)
	if err != nil || conflict.Outcome != wire.ReplaceOutcomeConflict || conflict.Snapshot == nil || conflict.Snapshot.Revision != applied.Snapshot.Revision {
		t.Fatal(conflict, err)
	}
}
