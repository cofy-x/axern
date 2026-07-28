package rollout

import (
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateAgentSelection(t *testing.T) {
	registry := agentcatalog.DefaultRegistry()
	claudeImage := "axern/claude-code-bundle@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	codexImage := "axern/codex-bundle@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	tests := []struct {
		name      string
		selection agent.Selection
		wantError bool
	}{
		{"sandbox-command local", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeSandboxCommand, BackendName: "local"}, false},
		{"agent-image contract", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: claudeImage, Profile: "deepseek"}, false},
		{"agent-image axern", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: claudeImage, Profile: "deepseek", BackendName: "axern"}, false},
		{"codex sandbox-command local", agent.Selection{Name: "codex", RuntimeType: domain.AgentRuntimeTypeSandboxCommand, BackendName: "local"}, false},
		{"codex agent-image", agent.Selection{Name: "codex", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: codexImage, Profile: "codex-smoke", BackendName: "axern"}, false},
		{"unknown agent", agent.Selection{Name: "unknown", RuntimeType: domain.AgentRuntimeTypeSandboxCommand, BackendName: "local"}, true},
		{"missing image", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeAgentImage, Profile: "deepseek", BackendName: "axern"}, true},
		{"missing profile", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: claudeImage, BackendName: "axern"}, true},
		{"local managed profile", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeSandboxCommand, Profile: "deepseek", BackendName: "local"}, false},
		{"local agent-image", agent.Selection{Name: "claude-code", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: claudeImage, Profile: "deepseek", BackendName: "local"}, true},
		{"codex missing image", agent.Selection{Name: "codex", RuntimeType: domain.AgentRuntimeTypeAgentImage, BackendName: "axern"}, true},
		{"codex missing profile", agent.Selection{Name: "codex", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: codexImage, BackendName: "axern"}, true},
		{"mutable image", agent.Selection{Name: "codex", RuntimeType: domain.AgentRuntimeTypeAgentImage, Image: "axern/codex-bundle:dev", Profile: "codex-smoke", BackendName: "axern"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateAgentSelection(registry, test.selection)
			if (err != nil) != test.wantError {
				t.Fatalf("validateAgentSelection() error = %v, wantError = %t", err, test.wantError)
			}
		})
	}
}
