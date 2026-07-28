package backend

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateAgentRuntimeSupportRejectsLocalAgentImage(t *testing.T) {
	err := ValidateAgentRuntimeSupport(string(NameLocal), domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeAgentImage,
			Image:   "axern/claude-code-bundle:dev",
			Profile: "deepseek",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "agent-image") || !strings.Contains(err.Error(), "local backend") {
		t.Fatalf("ValidateAgentRuntimeSupport error = %v, want local agent-image rejection", err)
	}
}

func TestValidateAgentRuntimeSupportAcceptsLocalProfileWithApproval(t *testing.T) {
	err := ValidateAgentRuntimeSupport(string(NameLocal), domain.AgentSpec{
		ApprovalPolicy: domain.AgentApprovalPolicyOnRequest,
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeSandboxCommand,
			Profile: "deepseek",
		},
	})
	if err != nil {
		t.Fatalf("ValidateAgentRuntimeSupport returned error: %v", err)
	}
}

func TestValidateAgentRuntimeSupportRejectsUnsafeApprovalIsolation(t *testing.T) {
	if err := ValidateAgentRuntimeSupport(string(NameLocal), domain.AgentSpec{ApprovalPolicy: domain.AgentApprovalPolicyNever}); err == nil {
		t.Fatal("local backend accepted approval policy never")
	}
	if err := ValidateAgentRuntimeSupport(string(NameAxern), domain.AgentSpec{ApprovalPolicy: domain.AgentApprovalPolicyOnRequest}); err == nil {
		t.Fatal("Axern backend accepted approval policy on_request")
	}
}

func TestValidateAgentRuntimeSupportAcceptsAxernBackendAgentImage(t *testing.T) {
	if err := ValidateAgentRuntimeSupport(string(NameAxern), domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeAgentImage,
			Image:   "axern/claude-code-bundle:dev",
			Profile: "deepseek",
		},
	}); err != nil {
		t.Fatalf("ValidateAgentRuntimeSupport returned error: %v", err)
	}
}

func TestValidateAgentRuntimeSupportRejectsAxernAgentImageWithoutImage(t *testing.T) {
	err := ValidateAgentRuntimeSupport(string(NameAxern), domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeAgentImage},
	})
	if err == nil || !strings.Contains(err.Error(), "requires agent bundle image") {
		t.Fatalf("ValidateAgentRuntimeSupport error = %v, want missing bundle image error", err)
	}
}
