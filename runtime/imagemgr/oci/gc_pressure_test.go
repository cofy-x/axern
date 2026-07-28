package oci

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGCLayersByDiskPressure_RemovesUnreferencedLayers(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	layer0Path := filepath.Join(mgr.layersDir, "sha256_unused", "fs")
	layer1Path := filepath.Join(mgr.layersDir, "sha256_used", "fs")
	if err := os.MkdirAll(layer0Path, 0755); err != nil {
		t.Fatalf("mkdir layer0: %v", err)
	}
	if err := os.MkdirAll(layer1Path, 0755); err != nil {
		t.Fatalf("mkdir layer1: %v", err)
	}

	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:unused", Path: layer0Path, RefCount: 0, LastUsedUnix: 1}); err != nil {
		t.Fatalf("put layer0: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{Digest: "sha256:used", Path: layer1Path, RefCount: 1, LastUsedUnix: 2}); err != nil {
		t.Fatalf("put layer1: %v", err)
	}

	calls := 0
	mgr.diskUsage = func(string) (float64, error) {
		calls++
		if calls == 1 {
			return 0.9, nil
		}
		return 0.7, nil
	}

	if err := mgr.gcLayersByDiskPressure(); err != nil {
		t.Fatalf("gcLayersByDiskPressure() error: %v", err)
	}

	unused, err := mgr.store.getLayer("sha256:unused")
	if err != nil {
		t.Fatalf("get unused layer: %v", err)
	}
	if unused != nil {
		t.Fatalf("unused layer should be deleted by gc")
	}
	used, err := mgr.store.getLayer("sha256:used")
	if err != nil {
		t.Fatalf("get used layer: %v", err)
	}
	if used == nil {
		t.Fatalf("used layer should remain")
	}
}

func TestGCChainsByDiskPressure_RemovesUnreferencedLowerdirs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	chain0Path := filepath.Join(mgr.chainsDir, "c-unused", "fs")
	chain1Path := filepath.Join(mgr.chainsDir, "c-used", "fs")
	if err := os.MkdirAll(chain0Path, 0755); err != nil {
		t.Fatalf("mkdir chain0: %v", err)
	}
	if err := os.MkdirAll(chain1Path, 0755); err != nil {
		t.Fatalf("mkdir chain1: %v", err)
	}

	if err := mgr.store.putChain(&ChainRecord{ChainID: "sha256:unused-chain", Path: chain0Path, RefCount: 0, LastUsedUnix: 1}); err != nil {
		t.Fatalf("put chain0: %v", err)
	}
	if err := mgr.store.putChain(&ChainRecord{ChainID: "sha256:used-chain", Path: chain1Path, RefCount: 1, LastUsedUnix: 2}); err != nil {
		t.Fatalf("put chain1: %v", err)
	}

	calls := 0
	mgr.diskUsage = func(string) (float64, error) {
		calls++
		if calls == 1 {
			return 0.9, nil
		}
		return 0.7, nil
	}

	if err := mgr.gcChainsByDiskPressure(); err != nil {
		t.Fatalf("gcChainsByDiskPressure() error: %v", err)
	}

	unused, err := mgr.store.getChain("sha256:unused-chain")
	if err != nil {
		t.Fatalf("get unused chain: %v", err)
	}
	if unused != nil {
		t.Fatalf("unused chain should be deleted by gc")
	}
	used, err := mgr.store.getChain("sha256:used-chain")
	if err != nil {
		t.Fatalf("get used chain: %v", err)
	}
	if used == nil {
		t.Fatalf("used chain should remain")
	}
}
