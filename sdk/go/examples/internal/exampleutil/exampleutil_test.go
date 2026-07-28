package exampleutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigResolveUsesCurrentContext(t *testing.T) {
	path := writeExampleConfig(t, `{
  "current_context": "compose",
  "contexts": {
    "compose": {
      "endpoint": "127.0.0.1:25000",
      "tls": {
        "ca_cert": "/tmp/ca.crt",
        "cert": "/tmp/client.crt",
        "key": "/tmp/client.key",
        "server_name": "gateway.local"
      },
      "proxy_mode": "direct"
    }
  }
}`)
	cfg := &Config{ConfigPath: path, Endpoint: "127.0.0.1:25000", TemplateID: "python311"}
	if err := cfg.applyContextDefaults(map[string]bool{}, func(string) string { return "" }); err != nil {
		t.Fatalf("applyContextDefaults: %v", err)
	}
	if cfg.ContextName != "compose" {
		t.Fatalf("ContextName = %q, want compose", cfg.ContextName)
	}
	if cfg.Endpoint != "127.0.0.1:25000" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.TLSCACert != "/tmp/ca.crt" || cfg.TLSCert != "/tmp/client.crt" || cfg.TLSKey != "/tmp/client.key" {
		t.Fatalf("TLS config not applied: %+v", cfg)
	}
	if cfg.TLSServerName != "gateway.local" || cfg.ProxyMode != "direct" {
		t.Fatalf("transport config not applied: %+v", cfg)
	}
}

func TestConfigResolvePreservesExplicitAndEnvValues(t *testing.T) {
	path := writeExampleConfig(t, `{
  "current_context": "compose",
  "contexts": {
    "compose": {
      "endpoint": "127.0.0.1:25000",
      "tls": {"ca_cert": "/tmp/ca.crt", "cert": "/tmp/client.crt", "key": "/tmp/client.key"}
    }
  }
}`)
	env := map[string]string{"AXERN_TLS_CA_CERT": "/env/ca.crt"}
	cfg := &Config{
		ConfigPath: path,
		Endpoint:   "explicit-target",
		TLSCACert:  "/env/ca.crt",
		TemplateID: "python311",
	}
	err := cfg.applyContextDefaults(map[string]bool{
		"endpoint": true,
	}, func(name string) string { return env[name] })
	if err != nil {
		t.Fatalf("applyContextDefaults: %v", err)
	}
	if cfg.Endpoint != "explicit-target" {
		t.Fatalf("Endpoint = %q, want explicit-target", cfg.Endpoint)
	}
	if cfg.TLSCACert != "/env/ca.crt" {
		t.Fatalf("TLSCACert = %q, want env value", cfg.TLSCACert)
	}
}

func TestConfigResolveAllowsMissingConfigWithoutContext(t *testing.T) {
	cfg := &Config{ConfigPath: filepath.Join(t.TempDir(), "missing.json"), Endpoint: "127.0.0.1:25000"}
	if err := cfg.applyContextDefaults(map[string]bool{}, func(string) string { return "" }); err != nil {
		t.Fatalf("applyContextDefaults: %v", err)
	}
	if cfg.Endpoint != "127.0.0.1:25000" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}
}

func TestConfigResolveErrorsForMissingRequestedContext(t *testing.T) {
	path := writeExampleConfig(t, `{"current_context":"missing","contexts":{}}`)
	cfg := &Config{ConfigPath: path}
	if err := cfg.applyContextDefaults(map[string]bool{}, func(string) string { return "" }); err == nil {
		t.Fatal("applyContextDefaults error = nil, want missing context error")
	}
}

func writeExampleConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
