package client

import (
	"errors"
	"testing"

	"github.com/openabstractions/abstraction-facade/go-core/bootstrap"
)

func TestDefaultConfigClientRefusesInstalledEndpointWithoutSeams(t *testing.T) {
	t.Setenv("OA_TEST_ALLOW_INSTALLED_RUNTIME", "")
	t.Setenv(EnvEndpoint, "")
	if DefaultEndpoint() != "" {
		t.Fatal("ambient config address escaped refusal")
	}
	for _, client := range []*Client{Discover(), New("")} {
		if _, err := client.Read(); !errors.Is(err, bootstrap.ErrNoTrustedInstallation) {
			t.Fatalf("ambient config read: %v", err)
		}
	}
}
