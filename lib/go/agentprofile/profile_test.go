package agentprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveReadsGenericAgentProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "agent_profiles": {
    "current_profile": "codex-smoke",
    "profiles": {
      "codex-smoke": {
        "agent": "codex",
        "provider": "openai",
        "wire_api": "responses",
        "upstream": "https://api.example.test/v1",
        "token": "sk-test",
        "template_id": "codex",
        "namespace": "dev",
        "remote_user": "axern",
        "env": {"CUSTOM": "1"},
        "config": {"reasoning_effort": "high"}
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	name, profile, ok, err := Resolve(path, "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !ok || name != "codex-smoke" ||
		profile.Agent != AgentCodex ||
		profile.ProviderType != ProviderOpenAI ||
		profile.WireAPI != WireAPIResponses ||
		profile.Upstream.String() != "https://api.example.test/v1" ||
		profile.Token != "sk-test" ||
		profile.TemplateID != "codex" ||
		profile.Namespace != "dev" ||
		profile.RemoteUser != "axern" ||
		profile.Env["CUSTOM"] != "1" ||
		profile.Config["reasoning_effort"] != "high" {
		t.Fatalf("name=%q ok=%t profile=%#v", name, ok, profile)
	}
}

func TestParseProfileRequiresWireAPI(t *testing.T) {
	_, err := ParseProfile("codex", &ProfileConfig{
		Agent:    "codex",
		Provider: "openai",
		Upstream: "https://api.example.test/v1",
		Token:    "sk-test",
	})
	if err == nil || !strings.Contains(err.Error(), "wire_api is required") {
		t.Fatalf("ParseProfile error = %v", err)
	}
}

func TestValidateWireAPI(t *testing.T) {
	if err := ValidateWireAPI(AgentCodex, WireAPIResponses); err != nil {
		t.Fatalf("ValidateWireAPI returned error: %v", err)
	}
	if err := ValidateWireAPI(AgentCodex, WireAPIAnthropicMessages); err == nil {
		t.Fatal("ValidateWireAPI error = nil")
	}
}

func TestResolveRejectsUnsupportedProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "agent_profiles": {
    "profiles": {
      "bad": {
        "agent": "codex",
        "provider": "unknown",
        "upstream": "https://api.example.test/v1",
        "token": "sk-test"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, _, _, err := Resolve(path, "bad"); err == nil {
		t.Fatal("Resolve error = nil")
	}
}

func TestParseUpstreamRejectsEmbeddedCredentials(t *testing.T) {
	_, err := ParseUpstream("agent profile", "https://user:secret@api.example.test/v1")
	if err == nil || !strings.Contains(err.Error(), "must not include user credentials") {
		t.Fatalf("ParseUpstream error = %v", err)
	}
}
