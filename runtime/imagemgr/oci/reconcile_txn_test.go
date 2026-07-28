package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileState_RecoversMountFromTxn(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layerPath := filepath.Join(mgr.layersDir, "sha256_txn", "fs")
	if err := os.MkdirAll(layerPath, 0755); err != nil {
		t.Fatalf("mkdir layer: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:txn", Path: layerPath, RefCount: 0}); err != nil {
		t.Fatalf("put layer: %v", err)
	}

	mountPath := filepath.Join(mgr.mountsDir, "txn-mount", "merged")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	if err := mgr.store.putMountTxn(&OciMountTxnRecord{
		ImageURL:     "docker.io/library/busybox:latest",
		MountID:      "txn-mount",
		MountPath:    mountPath,
		LayerDigests: []string{"sha256:txn"},
	}); err != nil {
		t.Fatalf("put mount txn: %v", err)
	}

	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{mountPath: {}}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	mount, err := mgr.store.getMount("docker.io/library/busybox:latest")
	if err != nil {
		t.Fatalf("get mount: %v", err)
	}
	if mount == nil {
		t.Fatalf("mount record should be recovered from txn")
	}
	txn, err := mgr.store.getMountTxn("docker.io/library/busybox:latest")
	if err != nil {
		t.Fatalf("get mount txn: %v", err)
	}
	if txn != nil {
		t.Fatalf("mount txn should be removed after recovery")
	}
	layer, err := mgr.store.getLayer("sha256:txn")
	if err != nil || layer == nil {
		t.Fatalf("get layer: err=%v", err)
	}
	if layer.RefCount != 1 {
		t.Fatalf("recovered mount should set refcount to 1, got %d", layer.RefCount)
	}
}

func TestReconcileState_RecoversMountFromTxnWithChainIDs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layerPath := filepath.Join(mgr.layersDir, "sha256_txn_chain", "fs")
	chainPath := filepath.Join(mgr.chainsDir, "c1", "fs")
	for _, path := range []string{layerPath, chainPath} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}
	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:txn-chain", Path: layerPath, RefCount: 0}); err != nil {
		t.Fatalf("put layer: %v", err)
	}
	if err := mgr.store.putChain(&ChainRecord{ChainID: "sha256:chain-txn", Path: chainPath, RefCount: 0}); err != nil {
		t.Fatalf("put chain: %v", err)
	}

	mountPath := filepath.Join(mgr.mountsDir, "txn-chain-mount", "merged")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	if err := mgr.store.putMountTxn(&OciMountTxnRecord{
		ImageURL:     "docker.io/library/busybox:chain",
		MountID:      "txn-chain-mount",
		MountPath:    mountPath,
		LayerDigests: []string{"sha256:txn-chain"},
		ChainIDs:     []string{"sha256:chain-txn"},
		LowerDirs:    []string{chainPath},
	}); err != nil {
		t.Fatalf("put mount txn: %v", err)
	}

	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{mountPath: {}}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	mount, err := mgr.store.getMount("docker.io/library/busybox:chain")
	if err != nil {
		t.Fatalf("get mount: %v", err)
	}
	if mount == nil {
		t.Fatalf("mount record should be recovered from txn")
	}
	if len(mount.ChainIDs) != 1 || mount.ChainIDs[0] != "sha256:chain-txn" {
		t.Fatalf("recovered mount chain ids mismatch, got %+v", mount.ChainIDs)
	}
	txn, err := mgr.store.getMountTxn("docker.io/library/busybox:chain")
	if err != nil {
		t.Fatalf("get mount txn: %v", err)
	}
	if txn != nil {
		t.Fatalf("mount txn should be removed after recovery")
	}
	layer, err := mgr.store.getLayer("sha256:txn-chain")
	if err != nil || layer == nil {
		t.Fatalf("get layer: err=%v", err)
	}
	if layer.RefCount != 1 {
		t.Fatalf("recovered mount should set layer refcount to 1, got %d", layer.RefCount)
	}
	chain, err := mgr.store.getChain("sha256:chain-txn")
	if err != nil || chain == nil {
		t.Fatalf("get chain: err=%v", err)
	}
	if chain.RefCount != 1 {
		t.Fatalf("recovered mount should set chain refcount to 1, got %d", chain.RefCount)
	}
}

func TestReconcileState_DropsTxnMountWhenLayersMissing(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	mountPath := filepath.Join(mgr.mountsDir, "txn-missing", "merged")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	if err := mgr.store.putMountTxn(&OciMountTxnRecord{
		ImageURL:     "docker.io/library/missing:latest",
		MountID:      "txn-missing",
		MountPath:    mountPath,
		LayerDigests: []string{"sha256:missing"},
	}); err != nil {
		t.Fatalf("put mount txn: %v", err)
	}

	unmounted := false
	mgr.unmountFn = func(target string) error {
		if target == mountPath {
			unmounted = true
		}
		return nil
	}
	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{mountPath: {}}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	mount, err := mgr.store.getMount("docker.io/library/missing:latest")
	if err != nil {
		t.Fatalf("get mount: %v", err)
	}
	if mount != nil {
		t.Fatalf("mount record should not be recovered when layers are missing")
	}
	txn, err := mgr.store.getMountTxn("docker.io/library/missing:latest")
	if err != nil {
		t.Fatalf("get mount txn: %v", err)
	}
	if txn != nil {
		t.Fatalf("mount txn should be removed when layers are missing")
	}
	if !unmounted {
		t.Fatalf("orphan txn mount should be unmounted when layers are missing")
	}
}
