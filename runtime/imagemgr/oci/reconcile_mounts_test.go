package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileState_FixesMountAndLayerRefs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layer1Path := filepath.Join(mgr.layersDir, "sha256_a", "fs")
	layer2Path := filepath.Join(mgr.layersDir, "sha256_b", "fs")
	if err := os.MkdirAll(layer1Path, 0755); err != nil {
		t.Fatalf("mkdir layer1: %v", err)
	}
	if err := os.MkdirAll(layer2Path, 0755); err != nil {
		t.Fatalf("mkdir layer2: %v", err)
	}

	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:a", Path: layer1Path, RefCount: 0}); err != nil {
		t.Fatalf("put layer1: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:b", Path: layer2Path, RefCount: 99}); err != nil {
		t.Fatalf("put layer2: %v", err)
	}

	mountPath := filepath.Join(mgr.mountsDir, "mount-1", "merged")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:     "docker.io/library/alpine:latest",
		MountID:      "mount-1",
		MountPath:    mountPath,
		LayerDigests: []string{"sha256:a", "sha256:b"},
	}); err != nil {
		t.Fatalf("put mount: %v", err)
	}

	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{mountPath: {}}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	mount, err := mgr.store.getMount("docker.io/library/alpine:latest")
	if err != nil {
		t.Fatalf("get mount: %v", err)
	}
	if mount == nil {
		t.Fatalf("mount should be kept after reconcile")
	}
	if _, ok := mgr.containers["docker.io/library/alpine:latest"]; !ok {
		t.Fatalf("in-memory containers should be restored from DB")
	}

	layer1, err := mgr.store.getLayer("sha256:a")
	if err != nil || layer1 == nil {
		t.Fatalf("get layer1 failed: err=%v", err)
	}
	layer2, err := mgr.store.getLayer("sha256:b")
	if err != nil || layer2 == nil {
		t.Fatalf("get layer2 failed: err=%v", err)
	}
	if layer1.RefCount != 1 || layer2.RefCount != 1 {
		t.Fatalf("layer refcounts not fixed, got %d and %d", layer1.RefCount, layer2.RefCount)
	}
}

func TestReconcileState_FixesChainRefsForRecoveredMount(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layer1Path := filepath.Join(mgr.layersDir, "sha256_chain_a", "fs")
	layer2Path := filepath.Join(mgr.layersDir, "sha256_chain_b", "fs")
	chain1Path := filepath.Join(mgr.chainsDir, "c1", "fs")
	chain2Path := filepath.Join(mgr.chainsDir, "c2", "fs")
	for _, path := range []string{layer1Path, layer2Path, chain1Path, chain2Path} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:a", Path: layer1Path, RefCount: 0}); err != nil {
		t.Fatalf("put layer1: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:b", Path: layer2Path, RefCount: 99}); err != nil {
		t.Fatalf("put layer2: %v", err)
	}
	if err := mgr.store.putChain(&ChainRecord{ChainID: "sha256:c1", Path: chain1Path, RefCount: 0}); err != nil {
		t.Fatalf("put chain1: %v", err)
	}
	if err := mgr.store.putChain(&ChainRecord{ChainID: "sha256:c2", Path: chain2Path, RefCount: 99}); err != nil {
		t.Fatalf("put chain2: %v", err)
	}

	mountPath := filepath.Join(mgr.mountsDir, "mount-chain", "merged")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:     "docker.io/library/alpine:chain",
		MountID:      "mount-chain",
		MountPath:    mountPath,
		LayerDigests: []string{"sha256:a", "sha256:b"},
		ChainIDs:     []string{"sha256:c1", "sha256:c2"},
		LowerDirs:    []string{chain2Path, chain1Path},
	}); err != nil {
		t.Fatalf("put mount: %v", err)
	}

	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{mountPath: {}}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	mount, err := mgr.store.getMount("docker.io/library/alpine:chain")
	if err != nil {
		t.Fatalf("get mount: %v", err)
	}
	if mount == nil {
		t.Fatalf("mount should be kept after reconcile")
	}
	if len(mount.ChainIDs) != 2 || mount.ChainIDs[0] != "sha256:c1" || mount.ChainIDs[1] != "sha256:c2" {
		t.Fatalf("mount chain ids not preserved, got %+v", mount.ChainIDs)
	}
	info, ok := mgr.containers["docker.io/library/alpine:chain"]
	if !ok {
		t.Fatalf("in-memory containers should be restored from DB")
	}
	if len(info.ChainIDs) != 2 || info.ChainIDs[0] != "sha256:c1" || info.ChainIDs[1] != "sha256:c2" {
		t.Fatalf("in-memory chain ids not restored, got %+v", info.ChainIDs)
	}

	chain1, err := mgr.store.getChain("sha256:c1")
	if err != nil || chain1 == nil {
		t.Fatalf("get chain1 failed: err=%v", err)
	}
	chain2, err := mgr.store.getChain("sha256:c2")
	if err != nil || chain2 == nil {
		t.Fatalf("get chain2 failed: err=%v", err)
	}
	if chain1.RefCount != 1 || chain2.RefCount != 1 {
		t.Fatalf("chain refcounts not fixed, got %d and %d", chain1.RefCount, chain2.RefCount)
	}
}

func TestReconcileState_DropsPersistedMountWhenMountMissing(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layerPath := filepath.Join(mgr.layersDir, "sha256_stale_mount", "fs")
	if err := os.MkdirAll(layerPath, 0755); err != nil {
		t.Fatalf("mkdir layer path: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:stale-mount", Path: layerPath, RefCount: 0}); err != nil {
		t.Fatalf("put layer: %v", err)
	}

	mountPath := filepath.Join(mgr.mountsDir, "stale-mount", "merged")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:     "docker.io/library/alpine:stale-mount",
		MountID:      "stale-mount",
		MountPath:    mountPath,
		LayerDigests: []string{"sha256:stale-mount"},
	}); err != nil {
		t.Fatalf("put mount: %v", err)
	}

	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{}, authoritative: true}, nil
	}

	if err := mgr.reconcileState(); err != nil {
		t.Fatalf("reconcileState() error: %v", err)
	}

	mount, err := mgr.store.getMount("docker.io/library/alpine:stale-mount")
	if err != nil {
		t.Fatalf("get mount: %v", err)
	}
	if mount != nil {
		t.Fatalf("stale persisted mount should be removed when no live mount exists")
	}
	if _, ok := mgr.containers["docker.io/library/alpine:stale-mount"]; ok {
		t.Fatalf("stale persisted mount should not be restored in memory")
	}
}
