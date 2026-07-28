package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileState_CleansOrphanOverlayMounts(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	orphanMountPath := filepath.Join(mgr.mountsDir, "orphan", "merged")
	if err := os.MkdirAll(orphanMountPath, 0755); err != nil {
		t.Fatalf("mkdir orphan mount: %v", err)
	}

	called := false
	mgr.unmountFn = func(target string) error {
		if target == orphanMountPath {
			called = true
		}
		return nil
	}
	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{orphanMountPath: {}}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}
	if !called {
		t.Fatalf("orphan overlay mount should be unmounted during reconcile")
	}
	if _, err := os.Stat(filepath.Dir(orphanMountPath)); !os.IsNotExist(err) {
		t.Fatalf("orphan mount directory should be removed, err=%v", err)
	}
}

func TestReconcileState_CleansStaleLayerTmpDirs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layerRoot := filepath.Join(mgr.layersDir, "sha256_stale")
	tmpPath := filepath.Join(layerRoot, "tmp-123")
	fsPath := filepath.Join(layerRoot, "fs")
	if err := os.MkdirAll(tmpPath, 0755); err != nil {
		t.Fatalf("mkdir tmp path: %v", err)
	}
	if err := os.MkdirAll(fsPath, 0755); err != nil {
		t.Fatalf("mkdir fs path: %v", err)
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("stale layer tmp dir should be removed, err=%v", err)
	}
	if _, err := os.Stat(fsPath); err != nil {
		t.Fatalf("layer fs dir should remain, err=%v", err)
	}
}
