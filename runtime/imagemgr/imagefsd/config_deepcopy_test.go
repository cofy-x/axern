package imagefsd

import "testing"

func TestBackendConfig_DeepCopy(t *testing.T) {
	tests := []struct {
		name string
		cfg  BackendConfig
	}{
		{
			name: "Registry config with proxy",
			cfg: BackendConfig{
				BackendType: "registry",
				Registry: &RegistryConfig{
					Host:        "docker.io",
					Repo:        "library/alpine",
					Auth:        "base64auth",
					Scheme:      "https",
					CaCertFiles: []string{"/proxy-ca/ca.crt"},
					Proxy: &ProxyConfig{
						Url:      "http://proxy:8080",
						Fallback: false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			copied := tt.cfg.DeepCopy()

			if copied.BackendType != tt.cfg.BackendType {
				t.Errorf("BackendType mismatch: got %s, want %s", copied.BackendType, tt.cfg.BackendType)
			}

			if tt.cfg.Registry != nil {
				if copied.Registry == tt.cfg.Registry {
					t.Error("Registry config was not deep copied (same pointer)")
				}
				if copied.Registry.Host != tt.cfg.Registry.Host {
					t.Errorf("Registry.Host mismatch: got %s, want %s", copied.Registry.Host, tt.cfg.Registry.Host)
				}
				if tt.cfg.Registry.Proxy != nil && copied.Registry.Proxy == tt.cfg.Registry.Proxy {
					t.Error("Registry.Proxy was not deep copied (same pointer)")
				}
				if len(tt.cfg.Registry.CaCertFiles) > 0 && &copied.Registry.CaCertFiles[0] == &tt.cfg.Registry.CaCertFiles[0] {
					t.Error("Registry.CaCertFiles was not deep copied")
				}
			}

			if copied.Registry != nil && len(copied.Registry.CaCertFiles) > 0 {
				copied.Registry.CaCertFiles[0] = "/modified/ca.crt"
				if tt.cfg.Registry.CaCertFiles[0] == "/modified/ca.crt" {
					t.Error("Modifying copied CA files affected original")
				}
			}
		})
	}
}
