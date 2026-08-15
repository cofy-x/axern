package command

import (
	"os"
	"path/filepath"
	"testing"

	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/spf13/cobra"
)

func TestConnectionConfigPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	document := `{
  "current_context": "hk",
  "contexts": {"hk": {
    "endpoint": "context:443",
    "tls": {"ca_cert": "context-ca", "cert": "context-cert", "key": "context-key", "server_name": "context-name"},
    "proxy_mode": "env"
  }}
}`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AXERN_ENDPOINT", "env:443")
	t.Setenv("AXERN_TLS_SERVER_NAME", "env-name")

	options := &Options{ConfigPath: path, Endpoint: "flag:443", ProxyMode: "direct"}
	root := &cobra.Command{Use: "test"}
	root.PersistentFlags().String("endpoint", "", "")
	root.PersistentFlags().String("tls-ca-cert", "", "")
	root.PersistentFlags().String("tls-cert", "", "")
	root.PersistentFlags().String("tls-key", "", "")
	root.PersistentFlags().String("tls-server-name", "", "")
	root.PersistentFlags().String("proxy-mode", "", "")
	if err := root.PersistentFlags().Set("endpoint", options.Endpoint); err != nil {
		t.Fatal(err)
	}
	if err := root.PersistentFlags().Set("proxy-mode", options.ProxyMode); err != nil {
		t.Fatal(err)
	}

	connection, err := (Runtime{Options: options, Root: root}).ResolveConnection()
	if err != nil {
		t.Fatal(err)
	}
	config := connection.Config
	if config.Endpoint != "flag:443" || config.ProxyMode != "direct" {
		t.Fatalf("flag overrides were not applied: %+v", config)
	}
	if config.TLSServerName != "env-name" || config.TLSCACert != "context-ca" {
		t.Fatalf("environment/context precedence is wrong: %+v", config)
	}
}

func TestConnectionConfigRejectsInvalidProxyMode(t *testing.T) {
	for _, name := range []string{"AXERN_ENDPOINT", "AXERN_TLS_CA_CERT", "AXERN_TLS_CERT", "AXERN_TLS_KEY"} {
		t.Setenv(name, "value")
	}
	t.Setenv("AXERN_PROXY_MODE", "disabled")
	runtime := Runtime{Options: &Options{ConfigPath: filepath.Join(t.TempDir(), "missing.json")}, Root: bareRoot()}
	if _, err := runtime.ResolveConnection(); err == nil {
		t.Fatal("ResolveConnection() error = nil")
	}
}

func TestConnectionConfigExplicitTransportDoesNotReadContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"control_target":"obsolete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"AXERN_ENDPOINT":    "gateway:443",
		"AXERN_TLS_CA_CERT": "ca.pem",
		"AXERN_TLS_CERT":    "client.pem",
		"AXERN_TLS_KEY":     "client-key.pem",
	} {
		t.Setenv(name, value)
	}

	connection, err := (Runtime{Options: &Options{ConfigPath: path}, Root: bareRoot()}).ResolveConnection()
	if err != nil {
		t.Fatal(err)
	}
	config := connection.Config
	if config.Endpoint != "gateway:443" || config.TLSCACert != "ca.pem" {
		t.Fatalf("ResolveConnection() config = %+v", config)
	}
}

func TestResolveConnectionIncludesResolvedContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{
  "current_context": "local",
  "contexts": {"local": {
    "endpoint": "gateway:443",
    "tls": {"ca_cert": "ca.pem", "cert": "client.pem", "key": "client-key.pem"}
  }}
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	connection, err := (Runtime{Options: &Options{ConfigPath: path}, Root: bareRoot()}).ResolveConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.ContextName != "local" || connection.Config.Endpoint != "gateway:443" {
		t.Fatalf("ResolveConnection() = %+v, want local gateway context", connection)
	}
}

func TestResolveConnectionExplicitTransportDoesNotReadContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"control_target":"obsolete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"AXERN_ENDPOINT":    "gateway:443",
		"AXERN_TLS_CA_CERT": "ca.pem",
		"AXERN_TLS_CERT":    "client.pem",
		"AXERN_TLS_KEY":     "client-key.pem",
	} {
		t.Setenv(name, value)
	}

	connection, err := (Runtime{Options: &Options{ConfigPath: path}, Root: bareRoot()}).ResolveConnection()
	if err != nil {
		t.Fatal(err)
	}
	if connection.ContextName != "" || connection.Config.Endpoint != "gateway:443" {
		t.Fatalf("ResolveConnection() = %+v, want context-free explicit transport", connection)
	}
}

func TestPinLocalEnvironmentImage(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AXERN_HOME", root)
	for _, name := range []string{"AXERN_ENDPOINT", "AXERN_TLS_CA_CERT", "AXERN_TLS_CERT", "AXERN_TLS_KEY"} {
		t.Setenv(name, "")
	}
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "current_context": "local",
	"contexts": {
		"local": {
			"endpoint": "gateway:443",
			"tls": {"ca_cert": "ca.pem", "cert": "client.pem", "key": "client-key.pem"}
		},
		"remote": {
			"endpoint": "remote:443",
			"tls": {"ca_cert": "ca.pem", "cert": "client.pem", "key": "client-key.pem"}
		}
	}
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	localDir := filepath.Join(root, "local")
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "image-references.json"), []byte(`{
  "version": 1,
  "references": {
    "demo:dev": {
      "canonical_ref": "index.docker.io/library/demo:dev",
      "immutable_ref": "index.docker.io/library/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "generation_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "updated_at": "2026-08-16T00:00:00Z"
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	runtime := Runtime{Options: &Options{ConfigPath: configPath}, Root: bareRoot()}
	spec := &environmentv1.EnvironmentSpec{Image: &environmentv1.EnvironmentImageSource{Ref: "demo:dev"}}
	if err := runtime.PinLocalEnvironmentImage(spec); err != nil {
		t.Fatalf("PinLocalEnvironmentImage() error = %v", err)
	}
	want := "index.docker.io/library/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if spec.GetImage().GetRef() != want {
		t.Fatalf("image ref = %q, want %q", spec.GetImage().GetRef(), want)
	}

	spec.Image.Ref = "demo:dev"
	spec.Image.RegistryCredentialID = "registry-secret"
	if err := runtime.PinLocalEnvironmentImage(spec); err == nil {
		t.Fatal("PinLocalEnvironmentImage() accepted a registry credential for a local generation")
	}

	remote := Runtime{Options: &Options{ConfigPath: configPath, ContextName: "remote"}, Root: bareRoot()}
	spec.Image.Ref = "demo:dev"
	spec.Image.RegistryCredentialID = ""
	if err := remote.PinLocalEnvironmentImage(spec); err != nil {
		t.Fatalf("remote PinLocalEnvironmentImage() error = %v", err)
	}
	if spec.GetImage().GetRef() != "demo:dev" {
		t.Fatalf("remote image ref = %q, want mutable registry ref", spec.GetImage().GetRef())
	}
}

func bareRoot() *cobra.Command {
	root := &cobra.Command{Use: "test"}
	for _, name := range []string{"endpoint", "tls-ca-cert", "tls-cert", "tls-key", "tls-server-name", "proxy-mode"} {
		root.PersistentFlags().String(name, "", "")
	}
	return root
}
