package imagefsd

import "testing"

func TestManager_GetDaemon(t *testing.T) {
	tmpDir := t.TempDir()

	ossConfig := BackendConfig{BackendType: "oss", Oss: &OssConfig{}}
	ossCfgPath := createTestConfigFile(t, tmpDir, "oss_config.json", ossConfig)

	nydusConfig := BackendConfig{BackendType: "registry", Registry: &RegistryConfig{}}
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

	opts := &DaemonCreateOpt{
		ID:   "test-get-daemon",
		Name: "test",
	}
	if err := mgr.CreateDaemon(opts); err != nil {
		t.Fatalf("CreateDaemon() failed: %v", err)
	}

	d := mgr.GetDaemon("test-get-daemon")
	if d == nil {
		t.Fatal("GetDaemon() returned nil for existing daemon")
	}
	if d.meta.ID != "test-get-daemon" {
		t.Errorf("Daemon ID = %s, want test-get-daemon", d.meta.ID)
	}
	if d.nodeID != "node-test" {
		t.Errorf("Daemon node ID = %s, want node-test", d.nodeID)
	}

	d = mgr.GetDaemon("non-existent")
	if d != nil {
		t.Error("GetDaemon() should return nil for non-existent daemon")
	}
}

func TestManager_ListDaemons(t *testing.T) {
	tmpDir := t.TempDir()

	ossConfig := BackendConfig{BackendType: "oss", Oss: &OssConfig{}}
	ossCfgPath := createTestConfigFile(t, tmpDir, "oss_config.json", ossConfig)

	nydusConfig := BackendConfig{BackendType: "registry", Registry: &RegistryConfig{}}
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

	list := mgr.ListDaemons()
	if len(list) != 0 {
		t.Errorf("Initial daemon list length = %d, want 0", len(list))
	}

	daemonIDs := []string{"daemon-1", "daemon-2", "daemon-3"}
	for _, id := range daemonIDs {
		if err := mgr.CreateDaemon(&DaemonCreateOpt{ID: id, Name: id}); err != nil {
			t.Fatalf("CreateDaemon(%s) failed: %v", id, err)
		}
	}

	list = mgr.ListDaemons()
	if len(list) != len(daemonIDs) {
		t.Errorf("Daemon list length = %d, want %d", len(list), len(daemonIDs))
	}

	foundIDs := make(map[string]bool)
	for _, info := range list {
		foundIDs[info.ID] = true
	}
	for _, id := range daemonIDs {
		if !foundIDs[id] {
			t.Errorf("Daemon %s not found in list", id)
		}
	}
}
