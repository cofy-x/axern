package agents

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

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
