package oci

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGCChainsByTTL_RemovesExpiredUnreferencedLowerdirs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	now := time.Unix(1700000000, 0)
	mgr.now = func() time.Time { return now }
	mgr.layerTTL = 30 * time.Minute

	expiredPath := filepath.Join(mgr.chainsDir, "c1", "fs")
	freshPath := filepath.Join(mgr.chainsDir, "c2", "fs")
	for _, p := range []string{expiredPath, freshPath} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(filepath.Join(p, "data"), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	if err := mgr.store.putChain(&ChainRecord{
		ChainID:       "sha256:expired",
		Path:          expiredPath,
		RefCount:      0,
		RefZeroAtUnix: now.Add(-31 * time.Minute).Unix(),
		LastUsedUnix:  now.Add(-31 * time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("put expired chain: %v", err)
	}
	if err := mgr.store.putChain(&ChainRecord{
		ChainID:       "sha256:fresh",
		Path:          freshPath,
		RefCount:      0,
		RefZeroAtUnix: now.Add(-10 * time.Minute).Unix(),
		LastUsedUnix:  now.Add(-10 * time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("put fresh chain: %v", err)
	}

	if err := mgr.gcChainsByTTL(); err != nil {
		t.Fatalf("gcChainsByTTL() error: %v", err)
	}

	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired chain path should be removed, err=%v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh chain path should remain, err=%v", err)
	}
	rec, err := mgr.store.getChain("sha256:expired")
	if err != nil {
		t.Fatalf("get expired chain: %v", err)
	}
	if rec != nil {
		t.Fatalf("expired chain metadata should be removed, got %+v", rec)
	}
}

func TestGCLayersByTTL_RemovesExpiredUnreferencedLayers(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	now := time.Unix(1700000000, 0)
	mgr.now = func() time.Time { return now }
	mgr.layerTTL = 30 * time.Minute

	expiredPath := filepath.Join(mgr.layersDir, "sha256_expired", "fs")
	freshPath := filepath.Join(mgr.layersDir, "sha256_fresh", "fs")
	usedPath := filepath.Join(mgr.layersDir, "sha256_used", "fs")
	for _, p := range []string{expiredPath, freshPath, usedPath} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(filepath.Join(p, "data"), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	if err := mgr.store.putLayer(&LayerRecord{
		Digest:        "sha256:expired",
		Path:          expiredPath,
		RefCount:      0,
		RefZeroAtUnix: now.Add(-31 * time.Minute).Unix(),
		LastUsedUnix:  now.Add(-31 * time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("put expired layer: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{
		Digest:        "sha256:fresh",
		Path:          freshPath,
		RefCount:      0,
		RefZeroAtUnix: now.Add(-10 * time.Minute).Unix(),
		LastUsedUnix:  now.Add(-10 * time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("put fresh layer: %v", err)
	}
	if err := mgr.store.putLayer(&LayerRecord{
		Digest:        "sha256:used",
		Path:          usedPath,
		RefCount:      1,
		RefZeroAtUnix: 0,
		LastUsedUnix:  now.Unix(),
	}); err != nil {
		t.Fatalf("put used layer: %v", err)
	}

	if err := mgr.gcLayers(); err != nil {
		t.Fatalf("gcLayers() error: %v", err)
	}

	expiredRec, err := mgr.store.getLayer("sha256:expired")
	if err != nil {
		t.Fatalf("get expired layer: %v", err)
	}
	if expiredRec != nil {
		t.Fatalf("expired layer should be deleted")
	}
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired layer path should be removed, err=%v", err)
	}

	freshRec, err := mgr.store.getLayer("sha256:fresh")
	if err != nil {
		t.Fatalf("get fresh layer: %v", err)
	}
	if freshRec == nil {
		t.Fatalf("fresh layer should not be deleted by TTL")
	}

	usedRec, err := mgr.store.getLayer("sha256:used")
	if err != nil {
		t.Fatalf("get used layer: %v", err)
	}
	if usedRec == nil {
		t.Fatalf("referenced layer should not be deleted by TTL")
	}
}
