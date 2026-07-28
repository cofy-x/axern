package imagefsd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNydusRuntimePolicy(t *testing.T) {
	policy := nydusRuntimePolicy{
		readaheadWorkers:     2,
		readaheadWindowBytes: 32 * 1024 * 1024,
		decodedCacheBytes:    8 * 1024 * 1024,
	}
	meta := DaemonMeta{
		ReadaheadWorkers:     1,
		ReadaheadWindowBytes: 4,
		DecodedCacheBytes:    8,
	}

	if policy.matches(&meta) {
		t.Fatal("policy unexpectedly matched stale daemon metadata")
	}
	policy.apply(&meta)
	if !policy.matches(&meta) {
		t.Fatal("applied policy did not match daemon metadata")
	}
}

func TestLoadExistedDaemonsReconcilesNydusRuntimePolicy(t *testing.T) {
	root := t.TempDir()
	daemonConfigDir := filepath.Join(root, "daemon_configs")
	if err := os.MkdirAll(daemonConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}

	backendPath := filepath.Join(root, "backend.json")
	backend, err := json.Marshal(BackendConfig{BackendType: "registry", Registry: &RegistryConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backendPath, backend, 0o644); err != nil {
		t.Fatal(err)
	}

	metaPath := filepath.Join(daemonConfigDir, "nydus-daemon.json")
	meta := DaemonMeta{
		ID:                   "nydus-daemon",
		Name:                 "test",
		SourceType:           SourceTypeNydus,
		CfgPath:              backendPath,
		PidFilePath:          filepath.Join(root, "missing.pid"),
		ReadaheadWorkers:     1,
		ReadaheadWindowBytes: 2,
		DecodedCacheBytes:    3,
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	mgr := &manager{
		ctx:                       context.Background(),
		nodeID:                    "node-test",
		root:                      root,
		binPath:                   "/usr/local/bin/imagefsd",
		daemons:                   make(map[string]*Daemon),
		nydusReadaheadWorkers:     4,
		nydusReadaheadWindowBytes: 5,
		nydusDecodedCacheBytes:    6,
	}
	if err := mgr.loadExistedDaemons(); err != nil {
		t.Fatal(err)
	}

	loaded := mgr.daemons[meta.ID]
	if loaded == nil {
		t.Fatal("recovered daemon not registered")
	}
	if !mgr.nydusRuntimePolicy().matches(&loaded.meta) {
		t.Fatalf("recovered daemon retained stale policy: %+v", loaded.meta)
	}
	if loaded.nodeID != "node-test" {
		t.Fatalf("recovered daemon node ID = %q, want node-test", loaded.nodeID)
	}

	persisted, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var persistedMeta DaemonMeta
	if err := json.Unmarshal(persisted, &persistedMeta); err != nil {
		t.Fatal(err)
	}
	if !mgr.nydusRuntimePolicy().matches(&persistedMeta) {
		t.Fatalf("persisted daemon retained stale policy: %+v", persistedMeta)
	}
}
