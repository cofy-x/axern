package imagefsd

import "testing"

func TestManager_CreateDaemon_OSS(t *testing.T) {
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
		Registry:    &RegistryConfig{},
	}
	nydusCfgPath := createTestConfigFile(t, tmpDir, "nydus_config.json", nydusConfig)

	ossAuthsPath := createTestOSSAuthsFile(t, tmpDir)
	registryAuthsPath := createTestRegistryAuthsFile(t, tmpDir)

	mgr, err := NewManager(&ManagerConfig{
		NodeID:            "node-test",
		Root:              tmpDir,
		OSSCfgPath:        ossCfgPath,
		NydusCfgPath:      nydusCfgPath,
		BinPath:           "/usr/local/bin/imagefsd",
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
			name: "create OSS daemon with explicit credentials",
			opts: &DaemonCreateOpt{
				ID:              "test-daemon-1",
				Name:            "test-image",
				Endpoint:        "oss-cn-hangzhou.aliyuncs.com",
				Bucket:          "my-bucket",
				ObjectPrefix:    "images/",
				AccessKeyID:     "explicit-key",
				AccessKeySecret: "explicit-secret",
			},
			validate: func(t *testing.T, d *Daemon) {
				if d.meta.ID != "test-daemon-1" {
					t.Errorf("Daemon ID = %s, want test-daemon-1", d.meta.ID)
				}
				if d.meta.SourceType != "oss" {
					t.Errorf("SourceType = %s, want oss", d.meta.SourceType)
				}
				if d.config.Oss.AccessKeyId != "explicit-key" {
					t.Errorf("AccessKeyId = %s, want explicit-key", d.config.Oss.AccessKeyId)
				}
			},
		},
		{
			name: "create OSS daemon with auto-populated credentials",
			opts: &DaemonCreateOpt{
				ID:           "test-daemon-2",
				Name:         "test-image-2",
				Endpoint:     "oss-cn-hangzhou.aliyuncs.com",
				Bucket:       "test-bucket",
				ObjectPrefix: "prefix/",
			},
			validate: func(t *testing.T, d *Daemon) {
				if d.config.Oss.AccessKeyId != "test-access-key" {
					t.Errorf("AccessKeyId = %s, want test-access-key (from auth file)", d.config.Oss.AccessKeyId)
				}
				if d.config.Oss.AccessKeySecret != "test-secret-key" {
					t.Errorf("AccessKeySecret = %s, want test-secret-key (from auth file)", d.config.Oss.AccessKeySecret)
				}
			},
		},
		{
			name: "create daemon without overwriting OSS config",
			opts: &DaemonCreateOpt{
				ID:   "test-daemon-3",
				Name: "test-image-3",
			},
			validate: func(t *testing.T, d *Daemon) {
				if d.config.Oss.Endpoint != "oss-default.aliyuncs.com" {
					t.Errorf("Endpoint = %s, want oss-default.aliyuncs.com (from template)", d.config.Oss.Endpoint)
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
}
