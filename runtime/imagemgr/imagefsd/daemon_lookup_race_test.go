package imagefsd

import (
	"sync"
	"testing"
)

func TestGetDaemon_ClearsMountFailed(t *testing.T) {
	d := newTestDaemon("get-clear")
	d.mountFailed.Store(true)

	mgr := newTestManager(map[string]*Daemon{
		d.meta.ID: d,
	})

	got := mgr.GetDaemon("get-clear")
	if got == nil {
		t.Fatal("GetDaemon should return the daemon")
	}
	if d.mountFailed.Load() {
		t.Error("GetDaemon should clear mountFailed to protect against GC race")
	}
}

func TestGetDaemon_NonExistent_ReturnsNil(t *testing.T) {
	mgr := newTestManager(map[string]*Daemon{})

	got := mgr.GetDaemon("no-such-daemon")
	if got != nil {
		t.Error("GetDaemon should return nil for non-existent daemon")
	}
}

func TestCreateDaemon_ExistingDaemon_ClearsMountFailed(t *testing.T) {
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
		t.Fatalf("NewManager failed: %v", err)
	}

	opts := &DaemonCreateOpt{ID: "existing-1", Name: "test"}
	if err := mgr.CreateDaemon(opts); err != nil {
		t.Fatalf("CreateDaemon() failed: %v", err)
	}

	d := mgr.GetDaemon(opts.ID)
	d.mountFailed.Store(true)

	if err := mgr.CreateDaemon(opts); err != nil {
		t.Fatalf("CreateDaemon() second call failed: %v", err)
	}

	if d.mountFailed.Load() {
		t.Error("CreateDaemon on existing daemon should clear mountFailed")
	}
}

func TestGCDaemons_RaceWithGetDaemon(t *testing.T) {
	d := newTestDaemon("race-1")
	d.mountFailed.Store(true)

	mgr := newTestManager(map[string]*Daemon{
		d.meta.ID: d,
	})

	got := mgr.GetDaemon("race-1")
	if got == nil {
		t.Fatal("GetDaemon should return the daemon")
	}

	mgr.gcDaemons()

	mgr.mu.RLock()
	_, exists := mgr.daemons[d.meta.ID]
	mgr.mu.RUnlock()

	if !exists {
		t.Error("GC should NOT delete daemon after GetDaemon cleared mountFailed")
	}
}

func TestGCDaemons_ConcurrentMountAndGC(t *testing.T) {
	for i := 0; i < 50; i++ {
		d := newTestDaemon("concurrent")
		d.mountFailed.Store(true)
		d.setState(DaemonStateRunning)
		d.isAliveFunc = func() bool { return true }

		mgr := newTestManager(map[string]*Daemon{
			d.meta.ID: d,
		})

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			mgr.gcDaemons()
		}()

		go func() {
			defer wg.Done()
			if got := mgr.GetDaemon("concurrent"); got != nil {
				_ = got.Mount()
			}
		}()

		wg.Wait()
	}
}
