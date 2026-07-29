package clientconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndResolveStrictContext(t *testing.T) {
	path := writeConfig(t, `{
  "current_context": "hk",
  "contexts": {
    "hk": {
      "endpoint": "gateway.example:443",
      "service_url": "https://services.example",
      "ssh_endpoint": "gateway.example:22",
      "ssh_identity_file": "/keys/hk",
      "tls": {"ca_cert": "/ca", "cert": "/cert", "key": "/key", "server_name": "gateway.example"},
      "proxy_mode": "direct"
    }
  }
}`)
	name, context, ok, err := Resolve(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || name != "hk" || context.Endpoint != "gateway.example:443" || context.ProxyMode != "direct" {
		t.Fatalf("Resolve() = %q, %+v, %t", name, context, ok)
	}
}

func TestLoadRejectsObsoleteAndUnknownFields(t *testing.T) {
	for name, document := range map[string]string{
		"root":    `{"current_context":"hk","obsolete":true,"contexts":{}}`,
		"context": `{"current_context":"hk","contexts":{"hk":{"endpoint":"gateway:443","control_target":"old:1","tls":{"ca_cert":"a","cert":"b","key":"c"}}}}`,
		"tls":     `{"current_context":"hk","contexts":{"hk":{"endpoint":"gateway:443","tls":{"ca_cert":"a","cert":"b","key":"c","relay_ca":"old"}}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeConfig(t, document))
			if err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestValidateRejectsInvalidProxyMode(t *testing.T) {
	err := Validate(&Context{Endpoint: "gateway:443", TLS: TLS{CACert: "a", Cert: "b", Key: "c"}, ProxyMode: "off"})
	if err == nil || !strings.Contains(err.Error(), "proxy_mode") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
