package agentprofile

import (
	"strings"
	"testing"

	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
)

func TestValidateCreateRequiresCompatibleAgentProviderAndWireAPI(t *testing.T) {
	tests := []struct {
		name     string
		agent    string
		provider agentprofilev1.AgentProvider
		wire     agentprofilev1.AgentWireApi
		wantErr  bool
	}{
		{"codex responses", "codex", agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI, agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES, false},
		{"claude messages", "claude-code", agentprofilev1.AgentProvider_AGENT_PROVIDER_ANTHROPIC, agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES, false},
		{"codex messages", "codex", agentprofilev1.AgentProvider_AGENT_PROVIDER_ANTHROPIC, agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES, true},
		{"claude responses", "claude-code", agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI, agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES, true},
		{"unknown agent", "custom", agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI, agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateCreate(CreateParams{
				Namespace: "default",
				Name:      "production",
				Spec: &agentprofilev1.AgentProfileSpec{
					Agent: test.agent, Provider: test.provider, WireApi: test.wire,
					BaseUrl: "https://provider.example/v1", MaxConcurrency: 1,
				},
				Credential: []byte("test-token"),
			})
			if test.wantErr && (err == nil || !strings.Contains(err.Error(), "incompatible")) {
				t.Fatalf("ValidateCreate() error = %v, want incompatibility", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("ValidateCreate() error = %v", err)
			}
		})
	}
}

func TestValidateCreateRejectsBaseURLQueryAndFragment(t *testing.T) {
	for _, baseURL := range []string{
		"https://provider.example/v1?token=secret",
		"https://provider.example/v1#fragment",
	} {
		err := ValidateCreate(CreateParams{
			Namespace: "default",
			Name:      "production",
			Spec: &agentprofilev1.AgentProfileSpec{
				Agent: "codex", Provider: agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI,
				WireApi: agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES,
				BaseUrl: baseURL, MaxConcurrency: 1,
			},
			Credential: []byte("test-token"),
		})
		if err == nil || !strings.Contains(err.Error(), "query, or fragment") {
			t.Fatalf("ValidateCreate(%q) error = %v", baseURL, err)
		}
	}
}
