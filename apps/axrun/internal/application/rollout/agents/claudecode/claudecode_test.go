package claudecode

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRegistrationValidatesAgentImageArtifactPolicy(t *testing.T) {
	reg := Registration()
	spec := domain.AgentSpec{
		Name: Name,
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeAgentImage,
			Image:   "axern/claude-code-bundle:dev",
			Profile: "deepseek",
			Artifacts: &domain.ArtifactPolicySpec{
				CaptureStdout: true,
				CaptureStderr: true,
				CaptureRawLog: true,
			},
		},
	}
	if err := reg.ValidateRuntime(spec); err != nil {
		t.Fatalf("ValidateRuntime returned error: %v", err)
	}

	spec.Runtime.Artifacts.CaptureRawLog = false
	err := reg.ValidateRuntime(spec)
	if err == nil || !strings.Contains(err.Error(), "raw log") {
		t.Fatalf("ValidateRuntime error = %v, want raw log error", err)
	}
}

func TestRegistrationRejectsNonCanonicalAgentImageMount(t *testing.T) {
	reg := Registration()
	spec := domain.AgentSpec{
		Name: Name,
		Runtime: &domain.AgentRuntimeSpec{
			Type:        domain.AgentRuntimeTypeAgentImage,
			MountTarget: "/opt/axern/agents/custom-claude",
		},
	}
	err := reg.ValidateRuntime(spec)
	if err == nil || !strings.Contains(err.Error(), "/opt/axern/agents/claude-code") {
		t.Fatalf("ValidateRuntime error = %v", err)
	}
}
