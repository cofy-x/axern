package imagefsd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	tmpDir := t.TempDir()

	ossConfig := BackendConfig{
		BackendType: "oss",
		Oss: &OssConfig{
			ObjectStoreCommon: ObjectStoreCommon{
				Endpoint:     "oss-default.aliyuncs.com",
				BucketName:   "default-bucket",
				ObjectPrefix: "default/",
			},
		},
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

	tests := []struct {
		name    string
		config  *ManagerConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &ManagerConfig{
				NodeID:            "node-test",
				Root:              tmpDir,
				OSSCfgPath:        ossCfgPath,
				NydusCfgPath:      nydusCfgPath,
				BinPath:           "/usr/local/bin/imagefsd",
				OSSAuthsPath:      ossAuthsPath,
				RegistryAuthsPath: registryAuthsPath,
			},
			wantErr: false,
		},
		{
			name: "missing node ID",
			config: &ManagerConfig{
				Root:              tmpDir,
				OSSCfgPath:        ossCfgPath,
				NydusCfgPath:      nydusCfgPath,
				BinPath:           "/usr/local/bin/imagefsd",
				OSSAuthsPath:      ossAuthsPath,
				RegistryAuthsPath: registryAuthsPath,
			},
			wantErr: true,
		},
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "missing OSS config",
			config: &ManagerConfig{
				NodeID:            "node-test",
				Root:              tmpDir,
				OSSCfgPath:        "",
				NydusCfgPath:      nydusCfgPath,
				BinPath:           "/usr/local/bin/imagefsd",
				OSSAuthsPath:      ossAuthsPath,
				RegistryAuthsPath: registryAuthsPath,
			},
			wantErr: true,
		},
		{
			name: "missing Nydus config",
			config: &ManagerConfig{
				NodeID:            "node-test",
				Root:              tmpDir,
				OSSCfgPath:        ossCfgPath,
				NydusCfgPath:      "",
				BinPath:           "/usr/local/bin/imagefsd",
				OSSAuthsPath:      ossAuthsPath,
				RegistryAuthsPath: registryAuthsPath,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewManager(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if mgr == nil {
					t.Fatal("NewManager() returned nil manager")
				}

				expectedDirs := []string{
					"chunk_db",
					"image_metas",
					"daemons",
					"daemon_configs",
					"daemon_log_staging",
				}
				for _, dir := range expectedDirs {
					path := filepath.Join(tmpDir, dir)
					if _, err := os.Stat(path); os.IsNotExist(err) {
						t.Errorf("Expected directory %s was not created", dir)
					}
				}
			}
		})
	}
}
