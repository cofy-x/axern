package imagefsd

import "testing"

func TestManager_CreateDaemon_S3Template(t *testing.T) {
	tmpDir := t.TempDir()

	s3Config := BackendConfig{
		BackendType: "s3",
		S3: &S3Config{
			ObjectStoreCommon: ObjectStoreCommon{
				Endpoint:     "minio:9000",
				BucketName:   "default-bucket",
				ObjectPrefix: "default/",
			},
			Region: "us-east-1",
		},
	}
	ossCfgPath := createTestConfigFile(t, tmpDir, "s3_config.json", s3Config)

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

	err = mgr.CreateDaemon(&DaemonCreateOpt{
		ID:              "s3-daemon",
		Name:            "rootfs.ext4",
		Endpoint:        "minio:9000",
		Bucket:          "test-bucket",
		ObjectPrefix:    "fixtures/",
		AccessKeyID:     "minioadmin",
		AccessKeySecret: "minioadmin",
	})
	if err != nil {
		t.Fatalf("CreateDaemon() failed: %v", err)
	}

	d := mgr.GetDaemon("s3-daemon")
	if d == nil {
		t.Fatal("Created daemon not found")
	}
	if d.config.S3 == nil {
		t.Fatal("S3 config is nil")
	}
	if d.config.S3.Endpoint != "minio:9000" {
		t.Errorf("Endpoint = %s, want minio:9000", d.config.S3.Endpoint)
	}
	if d.config.S3.Region != "us-east-1" {
		t.Errorf("Region = %s, want us-east-1", d.config.S3.Region)
	}
	if d.config.S3.AccessKeyId != "minioadmin" {
		t.Errorf("AccessKeyId = %s, want minioadmin", d.config.S3.AccessKeyId)
	}
}
