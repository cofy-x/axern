package nodeinventory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritableStorageInventorySeparatesFilesystemAndAllocationUsage(t *testing.T) {
	filestore := t.TempDir()
	requireDir := func(path string) {
		t.Helper()
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	requireDir(filepath.Join(filestore, "runc", "sandbox", "upper"))
	requireDir(filepath.Join(filestore, "reservations"))
	if err := os.WriteFile(filepath.Join(filestore, "runc", "sandbox", "upper", "data"), make([]byte, 8192), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filestore, "reservations", "sandbox.json"), []byte(`{"runtime_name":"runsc","request_bytes":4096}`), 0600); err != nil {
		t.Fatal(err)
	}
	source := NewAxnodedSource(AxnodedSourceOptions{
		StorageTargets: []StorageTarget{{Target: StorageTargetRuntimeFilestore, Path: filestore}},
		StatFS: func(string) (StorageInventoryEntry, error) {
			return StorageInventoryEntry{CapacityBytes: 1 << 20, UsedBytes: 512 << 10, AvailableBytes: 512 << 10}, nil
		},
	})
	snapshot := NewSnapshot()
	source.collectStorageInventory(time.Now().UTC(), &snapshot)
	entry := snapshot.Storage[0]
	if entry.AllocationUsedBytes <= 0 || entry.AllocationUsedBytes == entry.UsedBytes {
		t.Fatalf("allocation usage = %d, filesystem usage = %d", entry.AllocationUsedBytes, entry.UsedBytes)
	}
	if !entry.UnlinkedBackingUsageUnknown {
		t.Fatal("runsc reservation should expose possible unlinked backing usage")
	}
	if snapshot.Resources.EphemeralStorage.AxnodedUsedBytes != entry.AllocationUsedBytes {
		t.Fatalf("ephemeral storage resource usage = %d, want %d", snapshot.Resources.EphemeralStorage.AxnodedUsedBytes, entry.AllocationUsedBytes)
	}
}

func TestCollectStorageInventoryReportsEachTarget(t *testing.T) {
	now := time.Date(2026, 7, 7, 9, 0, 0, 0, time.UTC)
	source := NewAxnodedSource(AxnodedSourceOptions{
		StorageTargets: []StorageTarget{
			{Target: StorageTargetAxnodedState, Path: "/state"},
			{Target: StorageTargetImageCache, Path: "/images"},
		},
		StatFS: func(path string) (StorageInventoryEntry, error) {
			if path == "/images" {
				return StorageInventoryEntry{}, errors.New("permission denied")
			}
			return StorageInventoryEntry{
				CapacityBytes:   1000,
				UsedBytes:       300,
				AvailableBytes:  700,
				InodesTotal:     100,
				InodesUsed:      40,
				InodesAvailable: 60,
			}, nil
		},
	})
	snapshot := NewSnapshot()

	source.collectStorageInventory(now, &snapshot)

	if len(snapshot.Storage) != 2 {
		t.Fatalf("storage len = %d, want 2", len(snapshot.Storage))
	}
	state := snapshot.Storage[0]
	if state.Target != StorageTargetAxnodedState || state.Path != "/state" || !state.Collected {
		t.Fatalf("unexpected state target: %#v", state)
	}
	if state.CapacityBytes != 1000 || state.UsedBytes != 300 || state.InodesAvailable != 60 {
		t.Fatalf("unexpected state quantities: %#v", state)
	}
	images := snapshot.Storage[1]
	if images.Target != StorageTargetImageCache || images.Path != "/images" || images.Collected {
		t.Fatalf("unexpected image target: %#v", images)
	}
	if !strings.Contains(images.Error, "permission denied") {
		t.Fatalf("image error = %q, want permission denied", images.Error)
	}
	if got := snapshot.Sources["storage"]; got.Status != StatusDegraded || got.LastSuccessAt == nil || !got.LastSuccessAt.Equal(now) {
		t.Fatalf("storage source = %#v, want degraded at %v", got, now)
	}
}

func TestCollectStorageInventoryReportsErrorWhenAllTargetsFail(t *testing.T) {
	now := time.Date(2026, 7, 7, 9, 30, 0, 0, time.UTC)
	source := NewAxnodedSource(AxnodedSourceOptions{
		StorageTargets: []StorageTarget{
			{Target: StorageTargetAxnodedState, Path: "/state"},
			{Target: StorageTargetImageCache, Path: "/images"},
		},
		StatFS: func(path string) (StorageInventoryEntry, error) {
			return StorageInventoryEntry{}, errors.New("statfs failed for " + path)
		},
	})
	snapshot := NewSnapshot()

	source.collectStorageInventory(now, &snapshot)

	if got := snapshot.Sources["storage"]; got.Status != StatusError || got.LastSuccessAt != nil {
		t.Fatalf("storage source = %#v, want error without last success", got)
	}
	if len(snapshot.Storage) != 2 {
		t.Fatalf("storage len = %d, want 2", len(snapshot.Storage))
	}
	for _, entry := range snapshot.Storage {
		if entry.Collected || entry.Error == "" {
			t.Fatalf("unexpected failed storage entry: %#v", entry)
		}
	}
}

func TestNormalizeStorageTargetsDefaultsAndDeduplicates(t *testing.T) {
	defaults := normalizeStorageTargets(nil)
	if len(defaults) != 4 {
		t.Fatalf("default storage targets len = %d, want 4", len(defaults))
	}
	if defaults[0].Target != StorageTargetRootFS || defaults[0].Path != DefaultRootFSPath {
		t.Fatalf("unexpected first default target: %#v", defaults[0])
	}
	if defaults[1].Target != StorageTargetAxnodedState || defaults[1].Path != DefaultAxnodedStatePath {
		t.Fatalf("unexpected second default target: %#v", defaults[1])
	}

	targets := normalizeStorageTargets([]StorageTarget{
		{Target: " ", Path: "/skip"},
		{Target: StorageTargetAxnodedState, Path: "/state-a"},
		{Target: StorageTargetAxnodedState, Path: "/state-b"},
		{Target: StorageTargetVolumeData, Path: " "},
		{Target: StorageTargetImageCache, Path: "/images"},
	})
	if len(targets) != 2 {
		t.Fatalf("normalized storage targets = %#v, want 2 entries", targets)
	}
	if targets[0].Path != "/state-a" || targets[1].Path != "/images" {
		t.Fatalf("normalized storage targets = %#v", targets)
	}
}

func TestStorageStatMathProtectsBounds(t *testing.T) {
	if got := statBlocksToBytes(10, 4096); got != 40960 {
		t.Fatalf("statBlocksToBytes = %d, want 40960", got)
	}
	if got := statBlocksToBytes(10, 0); got != 0 {
		t.Fatalf("statBlocksToBytes with zero block size = %d, want 0", got)
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if got := statBlocksToBytes(uint64(maxInt64), 2); got != maxInt64 {
		t.Fatalf("statBlocksToBytes overflow = %d, want %d", got, maxInt64)
	}
	if got := safeUint64ToInt64(uint64(maxInt64) + 1); got != maxInt64 {
		t.Fatalf("safeUint64ToInt64 overflow = %d, want %d", got, maxInt64)
	}
}
