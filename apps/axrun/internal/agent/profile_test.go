package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/lib/go/agentprofile"
)

func TestLoadProfileReadsGenericAgentProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "agent_profiles": {
    "profiles": {
      "codex-smoke": {
        "agent": "codex",
        "provider": "openai",
        "wire_api": "responses",
        "upstream": "https://api.example.test/v1",
        "token": "sk-test",
        "env": {"CUSTOM": "1"},
        "config": {"reasoning_effort": "high"}
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	profile, err := LoadProfile(path, "codex-smoke")
	if err != nil {
		t.Fatalf("LoadProfile returned error: %v", err)
	}
	if profile.Name != "codex-smoke" ||
		profile.ProviderType != ProviderOpenAI ||
		profile.WireAPI != agentprofile.WireAPIResponses ||
		profile.Upstream.String() != "https://api.example.test/v1" ||
		profile.Token != "sk-test" ||
		profile.Env["CUSTOM"] != "1" ||
		profile.Config["reasoning_effort"] != "high" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestLoadProfileRejectsUnsupportedProvider(t *testing.T) {
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
	if _, err := LoadProfile(path, "bad"); err == nil {
		t.Fatal("LoadProfile error = nil")
	}
}
