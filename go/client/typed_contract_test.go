package client_test

import (
	"context"
	"reflect"
	"testing"

	wire "github.com/openabstractions/abstraction-config/go/abstraction/config"
	config "github.com/openabstractions/abstraction-config/go/client"
	rights "github.com/openabstractions/abstraction-rights/go/abstraction/rights/api"
)

// These assignments require exact aliases, including the nested result types.
var (
	_ func(*config.Client, context.Context, []string) (config.ReadResult, error)               = (*config.Client).ReadTypedContext
	_ func(*config.Client, context.Context, string, int64) (config.ConfigObservationV2, error) = (*config.Client).ObserveTypedContext
	_ wire.ReadResult                                                                          = config.ReadResult{Entries: []config.Entry{{
		Value: &config.Value{Kind: config.ValueKindBoolean},
		Origin: config.KeyOrigin{Location: config.LocationUser, State: config.KeyStateSet,
			Shadowed: []config.ConfigSource{{Location: config.LocationMachine}}},
	}}}
	_ wire.IgnoredField    = config.IgnoredField{Reason: config.IgnoredFieldReasonUnknownKey}
	_ wire.KeyProblem      = config.KeyProblem{}
	_ wire.KeyRefusal      = config.KeyRefusal{Reason: config.KeyRefusalReasonNotGranted}
	_ rights.RefusalDetail = config.RefusalDetail{Source: config.RefusalSourceRights,
		Decision: config.RefusalDecisionNotGranted, Ask: config.RefusalAskAvailable}
	_ wire.ReadResult = config.ReadResult{Refusal: &config.RefusalDetail{}}
)

func TestTypedVocabularyAliasesForwardGeneratedValues(t *testing.T) {
	checkValues(t, config.ValueKindValues, wire.ValueKindValues)
	checkValues(t, config.LocationValues, wire.LocationValues)
	checkValues(t, config.KeyStateValues, wire.KeyStateValues)
	checkValues(t, config.KeyRefusalReasonValues, wire.KeyRefusalReasonValues)
	checkValues(t, config.ReadOutcomeValues, wire.ReadOutcomeValues)
	checkValues(t, config.IgnoredFieldReasonValues, wire.IgnoredFieldReasonValues)
	checkValues(t, config.ObservationOutcomeV2Values, wire.ObservationOutcomeV2Values)
	checkValues(t, config.RefusalSourceValues, rights.RefusalSourceValues)
	checkValues(t, config.RefusalDecisionValues, rights.RefusalDecisionValues)
	checkValues(t, config.RefusalAskValues, rights.RefusalAskValues)
}

func checkValues[T comparable](t *testing.T, client, generated func() []T) {
	t.Helper()
	got, want := client(), generated()
	if !reflect.DeepEqual(got, want) || len(got) == 0 {
		t.Fatalf("client vocabulary %v, generated %v", got, want)
	}
	var zero T
	got[0] = zero
	if !reflect.DeepEqual(client(), want) || !reflect.DeepEqual(generated(), want) {
		t.Fatal("vocabulary call did not return a fresh generated value slice")
	}
}
