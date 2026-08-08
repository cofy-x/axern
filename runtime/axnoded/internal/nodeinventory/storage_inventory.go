package nodeinventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	StorageTargetRootFS           = "rootfs"
	StorageTargetAxnodedState     = "axnoded_state"
	StorageTargetImageCache       = "image_cache"
	StorageTargetVolumeData       = "volume_data"
	StorageTargetRuntimeFilestore = "runtime_filestore"

	DefaultRootFSPath       = "/"
	DefaultAxnodedStatePath = "/var/lib/axnoded"
	DefaultImageCachePath   = "/var/lib/imagemgr"
	DefaultVolumeDataPath   = "/var/lib/volumed"
)

type StorageTarget struct {
	Target             string
	Path               string
	SystemReserveBytes int64
}

func DefaultStorageTargets(axnodedStatePath string) []StorageTarget {
	axnodedStatePath = strings.TrimSpace(axnodedStatePath)
	if axnodedStatePath == "" {
		axnodedStatePath = DefaultAxnodedStatePath
	}
	return []StorageTarget{
		{Target: StorageTargetRootFS, Path: DefaultRootFSPath},
		{Target: StorageTargetAxnodedState, Path: axnodedStatePath},
		{Target: StorageTargetImageCache, Path: DefaultImageCachePath},
		{Target: StorageTargetVolumeData, Path: DefaultVolumeDataPath},
	}
}

func normalizeStorageTargets(targets []StorageTarget) []StorageTarget {
	if len(targets) == 0 {
		targets = DefaultStorageTargets("")
	}
	out := make([]StorageTarget, 0, len(targets))
	seen := map[string]struct{}{}
	for _, target := range targets {
		name := strings.TrimSpace(target.Target)
		path := strings.TrimSpace(target.Path)
		if name == "" || path == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, StorageTarget{Target: name, Path: path, SystemReserveBytes: target.SystemReserveBytes})
	}
	return out
}

func defaultStatFS(fn statfsFunc) statfsFunc {
	if fn != nil {
		return fn
	}
	return collectStorageStatFS
}

func (s *AxnodedSource) collectStorageInventory(now time.Time, snapshot *NodeInventorySnapshot) {
	if len(s.storageTargets) == 0 {
		snapshot.Sources["storage"] = SourceStatus{Status: StatusDisabled}
		return
	}
	successes := 0
	errs := make([]string, 0)
	for _, target := range s.storageTargets {
		entry, err := s.statFS(target.Path)
		entry.Target = target.Target
		entry.Path = target.Path
		if err != nil {
			entry.Collected = false
			entry.Error = err.Error()
			errs = append(errs, err.Error())
		} else {
			entry.Collected = true
			entry.SystemReserveBytes = target.SystemReserveBytes
			entry.AllocatableBytes = max(entry.CapacityBytes-target.SystemReserveBytes, 0)
			if target.Target == StorageTargetRuntimeFilestore {
				entry.ReservedBytes, entry.ActiveReservations = readWritableReservations(filepath.Join(target.Path, "reservations"))
				entry.AllocationUsedBytes = readVisibleWritableUsage(target.Path)
				entry.UnlinkedBackingUsageUnknown = hasRunscReservations(filepath.Join(target.Path, "reservations"))
				entry.FilesystemType, entry.MountIdentity = storageMountFacts(target.Path)
				snapshot.Resources.EphemeralStorage.AxnodedCommittedBytes = entry.ReservedBytes
				snapshot.Resources.EphemeralStorage.AxnodedUsedBytes = entry.AllocationUsedBytes
				snapshot.Node.Capacity.EphemeralStorageBytes = entry.CapacityBytes
				snapshot.Node.Allocatable.EphemeralStorageBytes = entry.AllocatableBytes
			}
			successes++
		}
		snapshot.Storage = append(snapshot.Storage, entry)
	}
	if len(errs) == 0 {
		snapshot.Sources["storage"] = readySource(now)
		return
	}
	sort.Strings(errs)
	errMsg := strings.Join(errs, "; ")
	if successes == 0 {
		snapshot.Sources["storage"] = errorSource(fmt.Errorf("%s", errMsg))
		return
	}
	snapshot.Sources["storage"] = degradedSource(errMsg, now)
}

func readVisibleWritableUsage(filestore string) int64 {
	var used int64
	for _, class := range []string{"projections", "runc", "runsc"} {
		_ = filepath.WalkDir(filepath.Join(filestore, class), func(_ string, entry os.DirEntry, err error) error {
			if err != nil || entry == nil {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return nil
			}
			if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Blocks > 0 {
				used += stat.Blocks * 512
			}
			return nil
		})
	}
	return used
}

func hasRunscReservations(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var value struct {
			RuntimeName string `json:"runtime_name"`
		}
		if json.Unmarshal(data, &value) == nil && value.RuntimeName == "runsc" {
			return true
		}
	}
	return false
}

func readWritableReservations(dir string) (int64, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var reserved, count int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var value struct {
			RequestBytes int64 `json:"request_bytes"`
		}
		if json.Unmarshal(data, &value) == nil && value.RequestBytes > 0 {
			reserved += value.RequestBytes
			count++
		}
	}
	return reserved, count
}

func collectStorageStatFS(path string) (StorageInventoryEntry, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return StorageInventoryEntry{}, fmt.Errorf("statfs %s: %w", path, err)
	}
	blockSize := int64(stat.Bsize)
	capacity := statBlocksToBytes(stat.Blocks, blockSize)
	available := statBlocksToBytes(stat.Bavail, blockSize)
	free := statBlocksToBytes(stat.Bfree, blockSize)
	used := capacity - free
	if used < 0 {
		used = 0
	}
	inodesTotal := safeUint64ToInt64(stat.Files)
	inodesAvailable := safeUint64ToInt64(stat.Ffree)
	inodesUsed := inodesTotal - inodesAvailable
	if inodesUsed < 0 {
		inodesUsed = 0
	}
	return StorageInventoryEntry{
		CapacityBytes:   capacity,
		UsedBytes:       used,
		AvailableBytes:  available,
		InodesTotal:     inodesTotal,
		InodesUsed:      inodesUsed,
		InodesAvailable: inodesAvailable,
	}, nil
}

func statBlocksToBytes(blocks uint64, blockSize int64) int64 {
	if blockSize <= 0 || blocks == 0 {
		return 0
	}
	size := uint64(blockSize)
	const maxInt64 = uint64(^uint64(0) >> 1)
	if blocks > maxInt64/size {
		return int64(maxInt64)
	}
	return int64(blocks * size)
}

func safeUint64ToInt64(value uint64) int64 {
	const maxInt64 = uint64(^uint64(0) >> 1)
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}
