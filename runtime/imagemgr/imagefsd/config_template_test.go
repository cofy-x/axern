package imagefsd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBackendConfig_LoadTemplate(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		wantErr     bool
		validate    func(*testing.T, *BackendConfig)
	}{
		{
			name: "valid registry config",
			fileContent: `{
				"type": "registry",
				"registry": {
					"host": "docker.io",
					"repo": "library/alpine",
					"scheme": "https"
				}
			}`,
			wantErr: false,
			validate: func(t *testing.T, cfg *BackendConfig) {
				if cfg.BackendType != "registry" {
					t.Errorf("BackendType = %s, want registry", cfg.BackendType)
				}
				if cfg.Registry == nil {
					t.Fatal("Registry config is nil")
				}
				if cfg.Registry.Host != "docker.io" {
					t.Errorf("Host = %s, want docker.io", cfg.Registry.Host)
				}
			},
		},
		{
			name:        "invalid JSON",
			fileContent: `{invalid json}`,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "config.json")

			if err := os.WriteFile(tmpFile, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("Failed to create temp file: %v", err)
			}

			cfg := &BackendConfig{}
			err := cfg.LoadTemplate(tmpFile)

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestProxyConfigMarshalPreservesDisabledFallback(t *testing.T) {
	data, err := json.Marshal(BackendConfig{
		BackendType: "registry",
		Registry: &RegistryConfig{
			CaCertFiles: []string{"/registry-proxy-ca/ca.crt"},
			Proxy: &ProxyConfig{
				Url:      "http://seed-client:4001",
				Fallback: false,
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	registry := document["registry"].(map[string]any)
	caCertFiles := registry["ca_cert_files"].([]any)
	if len(caCertFiles) != 1 || caCertFiles[0] != "/registry-proxy-ca/ca.crt" {
		t.Fatalf("ca_cert_files = %#v, want proxy CA path", caCertFiles)
	}
	proxy := registry["proxy"].(map[string]any)
	if fallback, ok := proxy["fallback"]; !ok || fallback != false {
		t.Fatalf("fallback = %#v, present = %v, want explicit false", fallback, ok)
	}
	if useHTTP, ok := proxy["use_http"]; ok {
		t.Fatalf("use_http = %#v, want field omitted", useHTTP)
	}
}
