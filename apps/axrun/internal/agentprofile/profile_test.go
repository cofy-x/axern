package agentprofile

import (
	"net/url"
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

func TestLoadRejectsInsecurePlaintextTokenPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"agent_profiles":{"profiles":{"codex":{"agent":"codex","provider":"openai","wire_api":"responses","upstream":"https://api.example.test/v1","token":"secret"}}}}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "owner-only permissions") {
		t.Fatalf("Load() error = %v, want permission rejection", err)
	}
}

func TestLoadRejectsSymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.json")
	data := []byte(`{"agent_profiles":{"profiles":{}}}`)
	if err := os.WriteFile(target, data, 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Load() error = %v, want symlink rejection", err)
	}
}

func TestSnapshotExcludesCredentialRotationButDetectsBehaviorChange(t *testing.T) {
	base := Profile{
		Name: "codex", Agent: AgentCodex, ProviderType: ProviderOpenAI,
		WireAPI: WireAPIResponses, Upstream: mustParseUpstream(t, "https://api.example.test/v1"),
		Token: "first-secret", Config: map[string]string{"reasoning_effort": "high"},
	}
	first, err := Snapshot(base)
	if err != nil {
		t.Fatal(err)
	}
	rotated := base
	rotated.Token = "second-secret"
	second, err := Snapshot(rotated)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("credential rotation changed behavior snapshot: first=%#v second=%#v", first, second)
	}
	changed := base
	changed.Config = map[string]string{"reasoning_effort": "medium"}
	third, err := Snapshot(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigFingerprint == third.ConfigFingerprint {
		t.Fatal("non-sensitive behavior change did not change fingerprint")
	}
	if strings.Contains(first.ConfigFingerprint, base.Token) {
		t.Fatal("snapshot fingerprint contains token")
	}
}

func TestParseProfileRejectsCredentialLikeEnvironment(t *testing.T) {
	_, err := ParseProfile("codex", &ProfileConfig{
		Agent: "codex", Provider: "openai", WireAPI: "responses",
		Upstream: "https://api.example.test/v1", Token: "secret",
		Env: map[string]string{"OPENAI_API_KEY": "another-secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "dedicated token field") {
		t.Fatalf("ParseProfile() error = %v", err)
	}
}

func TestParseProfileRejectsCredentialLikeConfig(t *testing.T) {
	_, err := ParseProfile("codex", &ProfileConfig{
		Agent: "codex", Provider: "openai", WireAPI: "responses",
		Upstream: "https://api.example.test/v1", Token: "secret",
		Config: map[string]string{"access_token": "another-secret"},
	})
	if err == nil || !strings.Contains(err.Error(), "dedicated token field") {
		t.Fatalf("ParseProfile() error = %v", err)
	}
}

func mustParseUpstream(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := ParseUpstream("test", value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
