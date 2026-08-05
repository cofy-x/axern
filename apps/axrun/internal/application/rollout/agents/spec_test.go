package agents

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateCanonicalAgentImageMount(t *testing.T) {
	spec := domain.AgentSpec{Runtime: &domain.AgentRuntimeSpec{
		Type:        domain.AgentRuntimeTypeAgentImage,
		MountTarget: "/opt/axern/agents/custom-codex",
	}}
	err := ValidateCanonicalAgentImageMount(spec, "codex")
	if err == nil || !strings.Contains(err.Error(), "/opt/axern/agents/codex") {
		t.Fatalf("ValidateCanonicalAgentImageMount error = %v", err)
	}

	spec.Runtime.MountTarget = "/opt/axern/agents/codex"
	if err := ValidateCanonicalAgentImageMount(spec, "codex"); err != nil {
		t.Fatalf("ValidateCanonicalAgentImageMount returned error: %v", err)
	}

	spec.Runtime.MountTarget = " /opt/axern/agents/codex "
	if err := ValidateCanonicalAgentImageMount(spec, "codex"); err == nil {
		t.Fatal("ValidateCanonicalAgentImageMount accepted a non-exact canonical target")
	}
}

func TestRuntimeSpecDoesNotInferContainerUserFromProfile(t *testing.T) {
	runtime := RuntimeSpec(SpecParams{
		Name:        "codex",
		Profile:     "deepseek-codex",
		RuntimeType: domain.AgentRuntimeTypeSandboxCommand,
	})

	if runtime.User != "" {
		t.Fatalf("runtime user = %q, want image default", runtime.User)
	}
}

func TestRuntimeSpecPreservesExplicitContainerUser(t *testing.T) {
	runtime := RuntimeSpec(SpecParams{
		Name:        "codex",
		Profile:     "deepseek-codex",
		RuntimeType: domain.AgentRuntimeTypeSandboxCommand,
		User:        "  runner  ",
	})

	if runtime.User != "runner" {
		t.Fatalf("runtime user = %q, want runner", runtime.User)
	}
}
