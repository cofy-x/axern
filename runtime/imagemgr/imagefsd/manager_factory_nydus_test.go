package imagefsd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestManager_CreateDaemon_Nydus(t *testing.T) {
	tmpDir := t.TempDir()

	ossConfig := BackendConfig{
		BackendType: "oss",
		Oss:         &OssConfig{},
	}
	ossCfgPath := createTestConfigFile(t, tmpDir, "oss_config.json", ossConfig)

	nydusConfig := BackendConfig{
		BackendType: "registry",
		Registry: &RegistryConfig{
			Host:   "docker.io",
			Scheme: "https",
			Auth:   "",
		},
	}
	nydusCfgPath := createTestConfigFile(t, tmpDir, "nydus_config.json", nydusConfig)

	ossAuthsPath := createTestOSSAuthsFile(t, tmpDir)
	registryAuthsPath := createTestRegistryAuthsFile(t, tmpDir)

	mockClient := &mockNydusClient{
		fetchAndExtractFunc: func(ctx context.Context, imageURL string, outputDir string) (string, error) {
			bootstrapPath := filepath.Join(outputDir, "bootstrap")
			if err := os.WriteFile(bootstrapPath, []byte("mock bootstrap"), 0644); err != nil {
				return "", err
			}
			return bootstrapPath, nil
		},
		useHTTPForFunc: func(imageURL string) bool {
			return imageURL == "localhost:5001/axern/fixture:dev"
		},
	}

	mgr, err := NewManager(&ManagerConfig{
		NodeID:            "node-test",
		Root:              tmpDir,
		OSSCfgPath:        ossCfgPath,
		NydusCfgPath:      nydusCfgPath,
		BinPath:           "/usr/local/bin/imagefsd",
		NydusClient:       mockClient,
		OSSAuthsPath:      ossAuthsPath,
		RegistryAuthsPath: registryAuthsPath,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	tests := []struct {
		name     string
		opts     *DaemonCreateOpt
		validate func(*testing.T, *Daemon)
	}{
		{
			name: "create Nydus daemon with registry auth lookup",
			opts: &DaemonCreateOpt{
				ID:         "nydus-daemon-1",
				Name:       "nydus-image",
				SourceType: "nydus",
				ImageURL:   "docker.io/library/alpine:latest",
			},
			validate: func(t *testing.T, d *Daemon) {
				if d.meta.SourceType != "nydus" {
					t.Errorf("SourceType = %s, want nydus", d.meta.SourceType)
				}
				if d.meta.BootstrapPath != "" {
					t.Error("BootstrapPath should be empty at creation time")
				}
				if d.meta.ImageURL != "docker.io/library/alpine:latest" {
					t.Errorf("ImageURL = %s, want docker.io/library/alpine:latest", d.meta.ImageURL)
				}
				if d.config.Registry.Host != "index.docker.io" {
					t.Errorf("Registry.Host = %s, want index.docker.io", d.config.Registry.Host)
				}
				if d.config.Registry.Repo != "library/alpine" {
					t.Errorf("Registry.Repo = %s, want library/alpine", d.config.Registry.Repo)
				}
				t.Logf("Registry.Auth = %s", d.config.Registry.Auth)
			},
		},
		{
			name: "create Nydus daemon with shared insecure registry policy",
			opts: &DaemonCreateOpt{
				ID:         "nydus-daemon-http",
				Name:       "nydus-image-http",
				SourceType: "nydus",
				ImageURL:   "localhost:5001/axern/fixture:dev",
			},
			validate: func(t *testing.T, d *Daemon) {
				if d.config.Registry.Scheme != "http" {
					t.Errorf("Registry.Scheme = %q, want http", d.config.Registry.Scheme)
				}
				if d.config.Registry.BlobUrlScheme != "http" {
					t.Errorf("Registry.BlobUrlScheme = %q, want http", d.config.Registry.BlobUrlScheme)
				}
			},
		},
		{
			name: "create Nydus daemon with specific repo auth",
			opts: &DaemonCreateOpt{
				ID:         "nydus-daemon-2",
				Name:       "nydus-image-2",
				SourceType: "nydus",
				ImageURL:   "reg.docker.alibaba-inc.com/namespace/repo:latest",
			},
			validate: func(t *testing.T, d *Daemon) {
				if d.config.Registry.Host != "reg.docker.alibaba-inc.com" {
					t.Errorf("Registry.Host = %s, want reg.docker.alibaba-inc.com", d.config.Registry.Host)
				}
				if d.config.Registry.Repo != "namespace/repo" {
					t.Errorf("Registry.Repo = %s, want namespace/repo", d.config.Registry.Repo)
				}
				if d.config.Registry.Auth != "base64-specific-repo-auth" {
					t.Errorf("Registry.Auth = %s, want base64-specific-repo-auth (repo-specific auth)", d.config.Registry.Auth)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mgr.CreateDaemon(tt.opts)
			if err != nil {
				t.Fatalf("CreateDaemon() failed: %v", err)
			}

			d := mgr.GetDaemon(tt.opts.ID)
			if d == nil {
				t.Fatal("Created daemon not found")
			}

			if tt.validate != nil {
				tt.validate(t, d)
			}
		})
	}
	if nydusConfig.Registry.Host != "docker.io" || nydusConfig.Registry.Repo != "" {
		t.Fatal("creating daemons mutated the Nydus config template")
	}
}
