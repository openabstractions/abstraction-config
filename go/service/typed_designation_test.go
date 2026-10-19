package service

import (
	"context"
	"errors"
	"testing"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	identity "github.com/openabstractions/abstraction-identity"
	rights "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// A reader no registration designates for a key is refused not_granted with
// identity detail; a designated reader the Rights rule refuses keeps Rights
// detail (CFG-E4). Before 2026-11-10 both read "rights not_granted", and the
// model host's log pointed at a missing rule when its declaration named no key.
func TestTypedReadNamesAMissingDesignationApartFromAMissingRule(t *testing.T) {
	for name, want := range map[string]rights.RefusalSource{
		"not designated": rights.RefusalSourceIdentity,
		"no rule":        rights.RefusalSourceRights,
	} {
		t.Run(name, func(t *testing.T) {
			transport, _, _ := typedTestHost(t, TypedOptions{ReadPolicy: func(context.Context, *identity.Peer, string) error {
				if name == "not designated" {
					return ErrKeyNotDesignated
				}
				return errors.New("rights: no rule")
			}})
			result, err := wire.NewConfigReaderV2Client(transport).Read([]string{"model_context"})
			if err != nil || result.Outcome != wire.ReadOutcomeRead || len(result.Refused) != 1 {
				t.Fatal(result, err)
			}
			refused := result.Refused[0]
			if refused.Reason != wire.KeyRefusalReasonNotGranted || refused.Refusal == nil || refused.Refusal.Source != want ||
				refused.Refusal.Decision != rights.RefusalDecisionNotGranted || refused.Refusal.Action != "abstraction.config/key.read" {
				t.Fatalf("refusal %+v, want not_granted with %s detail", refused.Refusal, want)
			}
		})
	}
}
