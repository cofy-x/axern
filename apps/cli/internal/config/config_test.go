package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/lib/go/agentprofile"
	"github.com/cofy-x/axern/lib/go/clientconfig"
)

func TestLoadMissingReturnsEmptyConfig(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected config")
	}
	if len(cfg.Contexts) != 0 {
		t.Fatalf("expected no contexts, got %d", len(cfg.Contexts))
	}
}

func TestSaveAndResolveContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &File{
		CurrentContext: "kind",
		Contexts: map[string]*clientconfig.Context{
			"kind": {
				Endpoint:        "127.0.0.1:24210",
				ServiceURL:      "http://127.0.0.1:25082",
				SSHEndpoint:     "127.0.0.1:25023",
				SSHIdentityFile: "/tmp/gateway_client_ed25519",
				TLS:             clientconfig.TLS{CACert: "/tmp/ca.crt", Cert: "/tmp/client.crt", Key: "/tmp/client.key"},
			},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	name, ctx, ok, err := Resolve(path, "")
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected context to resolve")
	}
	if name != "kind" {
		t.Fatalf("got context %q, want kind", name)
	}
	if ctx.Endpoint != "127.0.0.1:24210" {
		t.Fatalf("got endpoint %q", ctx.Endpoint)
	}
	if ctx.ServiceURL != "http://127.0.0.1:25082" {
		t.Fatalf("got service URL %q", ctx.ServiceURL)
	}
	if ctx.SSHEndpoint != "127.0.0.1:25023" {
		t.Fatalf("got ssh endpoint %q", ctx.SSHEndpoint)
	}
	if ctx.SSHIdentityFile != "/tmp/gateway_client_ed25519" {
		t.Fatalf("got ssh identity file %q", ctx.SSHIdentityFile)
	}
}

func TestAgentProfileStoresLocalToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := &File{
		AgentProfiles: agentprofile.ProfilesConfig{
			CurrentProfile: "deepseek",
			Profiles: map[string]*agentprofile.ProfileConfig{
				"deepseek": {
					Agent:    "codex",
					Provider: "openai",
					WireAPI:  "responses",
					Upstream: "https://api.example.test/anthropic",
					Token:    "sk-test-secret",
				},
			},
		},
	}
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config file permissions = %o, want 600", got)
	}
	name, profile, ok, err := ResolveAgentProfile(path, "")
	if err != nil {
		t.Fatalf("ResolveAgentProfile returned error: %v", err)
	}
	if !ok || name != "deepseek" {
		t.Fatalf("resolved name=%q ok=%t, want deepseek true", name, ok)
	}
	if profile.Token != "sk-test-secret" {
		t.Fatalf("Token = %q, want configured token", profile.Token)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if !bytes.Contains(raw, []byte("sk-test-secret")) {
		t.Fatal("config did not store token value")
	}
}

func TestOldClaudeCodeProfileSchemaIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "claude_code": {
    "current_profile": "deepseek",
    "profiles": {
      "deepseek": {
        "upstream": "https://api.example.test/anthropic",
        "token": "sk-test-secret"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, _, _, err := ResolveAgentProfile(path, ""); err == nil {
		t.Fatal("ResolveAgentProfile accepted an obsolete config schema")
	}
}

func TestSaveTightensExistingConfigPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := Save(path, &File{}); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat returned error: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config file permissions = %o, want 600", got)
	}
}
