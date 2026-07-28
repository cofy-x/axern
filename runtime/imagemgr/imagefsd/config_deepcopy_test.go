package imagefsd

import "testing"

func TestBackendConfig_DeepCopy(t *testing.T) {
	tests := []struct {
		name string
		cfg  BackendConfig
	}{
		{
			name: "OSS config with proxy",
			cfg: BackendConfig{
				BackendType: "oss",
				Oss: &OssConfig{
					ObjectStoreCommon: ObjectStoreCommon{
						Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
						BucketName:      "test-bucket",
						ObjectPrefix:    "prefix/",
						AccessKeyId:     "test-key",
						AccessKeySecret: "test-secret",
						Proxy: &ProxyConfig{
							Url:      "http://proxy:8080",
							Fallback: true,
						},
					},
				},
			},
		},
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
		{
			name: "S3 config with proxy",
			cfg: BackendConfig{
				BackendType: "s3",
				S3: &S3Config{
					ObjectStoreCommon: ObjectStoreCommon{
						Endpoint:        "minio:9000",
						BucketName:      "test-bucket",
						ObjectPrefix:    "prefix/",
						AccessKeyId:     "test-key",
						AccessKeySecret: "test-secret",
						Proxy: &ProxyConfig{
							Url:      "http://proxy:8080",
							Fallback: true,
						},
					},
					Region: "us-east-1",
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

			if tt.cfg.Oss != nil {
				if copied.Oss == tt.cfg.Oss {
					t.Error("Oss config was not deep copied (same pointer)")
				}
				if copied.Oss.Endpoint != tt.cfg.Oss.Endpoint {
					t.Errorf("Oss.Endpoint mismatch: got %s, want %s", copied.Oss.Endpoint, tt.cfg.Oss.Endpoint)
				}
				if tt.cfg.Oss.Proxy != nil && copied.Oss.Proxy == tt.cfg.Oss.Proxy {
					t.Error("Oss.Proxy was not deep copied (same pointer)")
				}
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

			if tt.cfg.S3 != nil {
				if copied.S3 == tt.cfg.S3 {
					t.Error("S3 config was not deep copied (same pointer)")
				}
				if copied.S3.Endpoint != tt.cfg.S3.Endpoint {
					t.Errorf("S3.Endpoint mismatch: got %s, want %s", copied.S3.Endpoint, tt.cfg.S3.Endpoint)
				}
				if tt.cfg.S3.Proxy != nil && copied.S3.Proxy == tt.cfg.S3.Proxy {
					t.Error("S3.Proxy was not deep copied (same pointer)")
				}
			}

			if copied.Oss != nil {
				copied.Oss.Endpoint = "modified-endpoint"
				if tt.cfg.Oss.Endpoint == "modified-endpoint" {
					t.Error("Modifying copied config affected original")
				}
			}
			if copied.S3 != nil {
				copied.S3.Endpoint = "modified-s3-endpoint"
				if tt.cfg.S3.Endpoint == "modified-s3-endpoint" {
					t.Error("Modifying copied S3 config affected original")
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
