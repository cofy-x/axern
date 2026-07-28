package local

import (
	"testing"

	agentpkg "github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRegisteredAgentReturnsHarness(t *testing.T) {
	reg := testRegistry()
	adapter := Adapter{
		AgentName: "oracle",
		Registry:  reg,
	}
	h, err := adapter.agentHarness(domain.AgentSpec{Name: "oracle"})
	if err != nil {
		t.Fatalf("agentHarness oracle: %v", err)
	}
	if h == nil {
		t.Fatal("agentHarness oracle = nil; want non-nil harness for built-in agent")
	}
}

func TestManagedAgentReturnsHarnessWithoutCommandGate(t *testing.T) {
	reg := testRegistry()
	adapter := Adapter{
		AgentName: "claude-code",
		Registry:  reg,
	}
	h, err := adapter.agentHarness(domain.AgentSpec{Name: "claude-code"})
	if err != nil {
		t.Fatalf("agentHarness claude-code: %v", err)
	}
	if h == nil {
		t.Fatal("agentHarness claude-code = nil; want managed harness")
	}
}

// TestAgentHarnessNilRegistryReturnsNil verifies that a nil registry is safe.
func TestAgentHarnessNilRegistryReturnsNil(t *testing.T) {
	adapter := Adapter{AgentName: "oracle", Registry: nil}
	h, err := adapter.agentHarness(domain.AgentSpec{Name: "oracle"})
	if err != nil {
		t.Fatalf("agentHarness with nil registry: %v", err)
	}
	if h != nil {
		t.Fatal("agentHarness with nil registry = non-nil, want nil")
	}
}

// TestAgentHarnessUnknownAgentReturnsNil verifies that an agent not present
// in the registry is handled gracefully.
func TestAgentHarnessUnknownAgentReturnsNil(t *testing.T) {
	reg := agentpkg.NewRegistry()
	adapter := Adapter{AgentName: "mystery-agent", Registry: reg}
	h, err := adapter.agentHarness(domain.AgentSpec{Name: "mystery-agent"})
	if err != nil {
		t.Fatalf("agentHarness unknown: %v", err)
	}
	if h != nil {
		t.Fatal("agentHarness unknown = non-nil, want nil")
	}
}
