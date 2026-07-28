package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestEnsureChainLowerDirs_CreatesDockerStyleLowerdirs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	hash1, err := v1.NewHash(fmt.Sprintf("sha256:%064x", 1))
	if err != nil {
		t.Fatalf("new hash1: %v", err)
	}
	hash2, err := v1.NewHash(fmt.Sprintf("sha256:%064x", 2))
	if err != nil {
		t.Fatalf("new hash2: %v", err)
	}

	layers := []v1.Layer{
		sleepLayer{digest: hash1},
		sleepLayer{digest: hash2},
		sleepLayer{digest: hash2},
	}

	layerDigests, layerPaths, err := mgr.extractLayersWithWorkers(context.Background(), layers)
	if err != nil {
		t.Fatalf("extractLayersWithWorkers() error: %v", err)
	}
	if layerDigests[1] != layerDigests[2] {
		t.Fatalf("expected duplicate digests, got %s and %s", layerDigests[1], layerDigests[2])
	}
	if layerPaths[1] != layerPaths[2] {
		t.Fatalf("expected duplicate cache paths before materialization, got %s and %s", layerPaths[1], layerPaths[2])
	}

	diffIDs := []v1.Hash{hash1, hash2, hash2}
	chainIDs, err := buildChainIDs(diffIDs)
	if err != nil {
		t.Fatalf("buildChainIDs() error: %v", err)
	}
	if chainIDs[1] == chainIDs[2] {
		t.Fatalf("expected duplicate layer occurrence to produce distinct chainIDs, got %s", chainIDs[1])
	}

	gotPaths, err := mgr.ensureChainLowerDirs(chainIDs, layerPaths)
	if err != nil {
		t.Fatalf("ensureChainLowerDirs() error: %v", err)
	}
	gotPathsAgain, err := mgr.ensureChainLowerDirs(chainIDs, layerPaths)
	if err != nil {
		t.Fatalf("ensureChainLowerDirs() second call error: %v", err)
	}
	if gotPathsAgain[1] != gotPaths[1] || gotPathsAgain[2] != gotPaths[2] {
		t.Fatalf("expected stable chain lowerdir mapping, got %v then %v", gotPaths, gotPathsAgain)
	}

	if gotPaths[0] == layerPaths[0] {
		t.Fatalf("expected chain lowerdir path to differ from source layer path, got %s", gotPaths[0])
	}
	if gotPaths[1] == layerPaths[1] {
		t.Fatalf("expected chain lowerdir path to differ from source layer path, got %s", gotPaths[1])
	}
	if gotPaths[2] == layerPaths[2] {
		t.Fatalf("expected repeated occurrence to use chain lowerdir path, got %s", gotPaths[2])
	}
	if !strings.HasPrefix(gotPaths[2], mgr.chainsDir) {
		t.Fatalf("expected chain lowerdir path %s to live under %s", gotPaths[2], mgr.chainsDir)
	}
	if _, err := os.Stat(filepath.Join(gotPaths[2], "etc", "config")); err != nil {
		t.Fatalf("duplicate occurrence content missing: %v", err)
	}
	sourceInfo, err := os.Stat(filepath.Join(layerPaths[1], "etc", "config"))
	if err != nil {
		t.Fatalf("stat source config: %v", err)
	}
	targetInfo, err := os.Stat(filepath.Join(gotPaths[1], "etc", "config"))
	if err != nil {
		t.Fatalf("stat chain config: %v", err)
	}
	if !os.SameFile(sourceInfo, targetInfo) {
		t.Fatalf("expected chain lowerdir file to be hardlinked from source layer")
	}
}

func TestEnsureChainLowerDirs_RollsBackReservedRefsOnError(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	firstHash, err := v1.NewHash(fmt.Sprintf("sha256:%064x", 11))
	if err != nil {
		t.Fatalf("new hash1: %v", err)
	}
	secondHash, err := v1.NewHash(fmt.Sprintf("sha256:%064x", 12))
	if err != nil {
		t.Fatalf("new hash2: %v", err)
	}

	firstPath := filepath.Join(mgr.layersDir, "first", "fs")
	if err := os.MkdirAll(filepath.Join(firstPath, "etc"), 0755); err != nil {
		t.Fatalf("mkdir first path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstPath, "etc", "config"), []byte("first"), 0644); err != nil {
		t.Fatalf("write first config: %v", err)
	}

	chainIDs, err := buildChainIDs([]v1.Hash{firstHash, secondHash})
	if err != nil {
		t.Fatalf("buildChainIDs() error: %v", err)
	}

	if _, err := mgr.ensureChainLowerDirs(chainIDs, []string{firstPath, filepath.Join(mgr.layersDir, "missing", "fs")}); err == nil {
		t.Fatalf("ensureChainLowerDirs() error = nil, want non-nil")
	}

	firstChain, err := mgr.store.getChain(chainIDs[0])
	if err != nil {
		t.Fatalf("get first chain: %v", err)
	}
	if firstChain == nil {
		t.Fatalf("expected first chain metadata to exist")
	}
	if firstChain.RefCount != 0 {
		t.Fatalf("expected first chain ref rollback to restore refcount 0, got %d", firstChain.RefCount)
	}
	if firstChain.RefZeroAtUnix == 0 {
		t.Fatalf("expected first chain ref-zero timestamp to be set after rollback")
	}
}

func TestMaterializeChainLowerDir_UsesManagerClockForTempDir(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	fixedNow := time.Unix(1700000000, 123456789)
	mgr.now = func() time.Time { return fixedNow }

	sourcePath := filepath.Join(mgr.layersDir, "clock-source", "fs")
	targetPath := filepath.Join(mgr.chainsDir, "clock-target", "fs")
	if err := os.MkdirAll(filepath.Join(sourcePath, "etc"), 0755); err != nil {
		t.Fatalf("mkdir source path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourcePath, "etc", "config"), []byte("clock"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	expectedTmpPath := filepath.Join(filepath.Dir(targetPath), fmt.Sprintf("tmp-%d", fixedNow.UnixNano()))
	if err := os.MkdirAll(expectedTmpPath, 0755); err != nil {
		t.Fatalf("mkdir stale tmp path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(expectedTmpPath, "stale"), []byte("old"), 0644); err != nil {
		t.Fatalf("write stale tmp file: %v", err)
	}

	if err := mgr.materializeChainLowerDir(sourcePath, targetPath); err != nil {
		t.Fatalf("materializeChainLowerDir() error: %v", err)
	}

	if _, err := os.Stat(expectedTmpPath); !os.IsNotExist(err) {
		t.Fatalf("expected manager-clock temp path to be removed, err=%v", err)
	}
	content, err := os.ReadFile(filepath.Join(targetPath, "etc", "config"))
	if err != nil {
		t.Fatalf("read target file: %v", err)
	}
	if string(content) != "clock" {
		t.Fatalf("target file content = %q, want %q", string(content), "clock")
	}
}
