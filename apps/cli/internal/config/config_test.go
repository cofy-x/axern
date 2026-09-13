package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/sdk/go/clientconfig"
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
	if ctx.SSHEndpoint != "127.0.0.1:25023" {
		t.Fatalf("got ssh endpoint %q", ctx.SSHEndpoint)
	}
	if ctx.SSHIdentityFile != "/tmp/gateway_client_ed25519" {
		t.Fatalf("got ssh identity file %q", ctx.SSHIdentityFile)
	}
}

func TestUnknownTopLevelFieldsAreRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "unknown_product_config": {"enabled": true}
}`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load accepted an unknown top-level field")
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
