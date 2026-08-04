package codex

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRegistrationRejectsNonCanonicalAgentImageMount(t *testing.T) {
	reg := Registration()
	spec := domain.AgentSpec{
		Name: Name,
		Runtime: &domain.AgentRuntimeSpec{
			Type:        domain.AgentRuntimeTypeAgentImage,
			MountTarget: "/opt/axern/agents/custom-codex",
		},
	}
	err := reg.ValidateRuntime(spec)
	if err == nil || !strings.Contains(err.Error(), "/opt/axern/agents/codex") {
		t.Fatalf("ValidateRuntime error = %v", err)
	}
}
