package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestImportKubernetesSecretWritesPrivateCertificates(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	certDir := filepath.Join(dir, "contexts", "local")
	payload := kubernetesSecretPayload(t, map[string]string{
		"ca.crt": "ca", "client.crt": "cert", "client.key": "key",
	})
	if err := ImportKubernetesSecret(payload, KubernetesImportParams{
		Name: "local", ConfigPath: configPath, CertDir: certDir,
		Endpoint: "127.0.0.1:25100", ServiceURL: "http://127.0.0.1:25101",
		SSHEndpoint: "127.0.0.1:25122", ProxyMode: "direct", Current: true,
	}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ca.crt", "client.crt", "client.key"} {
		info, err := os.Stat(filepath.Join(certDir, key))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o, want 600", key, info.Mode().Perm())
		}
	}
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "local" || cfg.Contexts["local"].ProxyMode != "direct" {
		t.Fatalf("unexpected imported config: %+v", cfg)
	}
}

func TestImportKubernetesSecretRejectsInvalidDataBeforeWriting(t *testing.T) {
	t.Parallel()
	certDir := filepath.Join(t.TempDir(), "certs")
	payload := kubernetesSecretPayload(t, map[string]string{"ca.crt": "ca"})
	err := ImportKubernetesSecret(payload, KubernetesImportParams{
		Name: "local", ConfigPath: filepath.Join(t.TempDir(), "config.json"), CertDir: certDir, Endpoint: "localhost:1",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, statErr := os.Stat(certDir); !os.IsNotExist(statErr) {
		t.Fatalf("certificate directory was created after invalid input: %v", statErr)
	}
}

func TestImportKubernetesSecretRejectsUnsafeContextName(t *testing.T) {
	t.Parallel()
	err := ImportKubernetesSecret(nil, KubernetesImportParams{Name: "../outside", CertDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func kubernetesSecretPayload(t *testing.T, values map[string]string) []byte {
	t.Helper()
	data := make(map[string]string, len(values))
	for key, value := range values {
		data[key] = base64.StdEncoding.EncodeToString([]byte(value))
	}
	payload, err := json.Marshal(kubernetesSecret{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
