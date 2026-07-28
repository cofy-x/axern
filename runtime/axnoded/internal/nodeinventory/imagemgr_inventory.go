package nodeinventory

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *AxnodedSource) collectImagemgrInventory(now time.Time, snapshot *NodeInventorySnapshot) {
	if s.imageManager == nil || !s.imageManager.Enabled() {
		snapshot.Sources["imagemgr"] = SourceStatus{Status: StatusDisabled}
		snapshot.Sources["imagefsd"] = SourceStatus{Status: StatusDisabled}
		snapshot.Components.Imagemgr.Status = StatusDisabled
		snapshot.Components.Imagefsd.Status = StatusDisabled
		return
	}

	inventory, err := s.imageManager.Inventory(context.Background())
	if err != nil {
		status := errorSource(err)
		snapshot.Sources["imagemgr"] = status
		snapshot.Sources["imagefsd"] = status
		snapshot.Components.Imagemgr.Status = StatusError
		snapshot.Components.Imagemgr.Error = err.Error()
		snapshot.Components.Imagefsd.Status = StatusError
		snapshot.Components.Imagefsd.Error = err.Error()
		return
	}

	snapshot.Sources["imagemgr"] = readySource(now)
	snapshot.Components.Imagemgr.Status = StatusReady
	snapshot.Components.Imagemgr.Reachable = true
	snapshot.Components.Imagemgr.DaemonCount = len(inventory.Daemons)
	snapshot.Components.Imagemgr.MountedImageCount = len(inventory.MountedImages)
	snapshot.Components.Imagemgr.ImportedImageCount = len(inventory.ImportedImages)

	mergeImagemgrLocality(snapshot, inventory.Locality)

	mountedURLs := make([]string, 0, len(inventory.MountedImages))
	for _, mount := range inventory.MountedImages {
		mountedURLs = append(mountedURLs, mount.ImageURL)
	}
	sort.Strings(mountedURLs)
	snapshot.Heat.MountedImageURLs = mountedURLs
	snapshot.Heat.MountedRootfsCount = len(inventory.MountedImages)

	for _, daemon := range inventory.Daemons {
		if daemon.SourceType == "nydus" {
			snapshot.Heat.NydusDaemonCount++
		}
	}

	imagefsdErr := strings.TrimSpace(strings.Join(compactStrings([]string{
		inventory.ChunkDBError,
		inventory.LocalityError,
	}), "; "))
	if imagefsdErr != "" {
		snapshot.Sources["imagefsd"] = errorSource(fmt.Errorf("%s", imagefsdErr))
		snapshot.Components.Imagefsd.Status = StatusError
		snapshot.Components.Imagefsd.Error = imagefsdErr
	} else {
		snapshot.Sources["imagefsd"] = readySource(now)
		snapshot.Components.Imagefsd.Status = StatusReady
		snapshot.Components.Imagefsd.Reachable = true
	}
	if inventory.ChunkDB == nil {
		return
	}

	usagePercent, _ := strconv.ParseFloat(inventory.ChunkDB.Storage.UsagePercent, 64)
	snapshot.Components.Imagefsd.ChunkDBPresent = true
	snapshot.Components.Imagefsd.ChunkCount = inventory.ChunkDB.Chunks.TotalCount
	snapshot.Components.Imagefsd.ChunkDBUsedBytes = inventory.ChunkDB.Storage.UsedSizeBytes
	snapshot.Components.Imagefsd.ChunkDBUsagePercent = usagePercent
	if imagefsdErr == "" {
		snapshot.Components.Imagefsd.Reachable = true
	}

	snapshot.Heat.ChunkDB.TotalChunks = inventory.ChunkDB.Chunks.TotalCount
	snapshot.Heat.ChunkDB.UsedBytes = inventory.ChunkDB.Storage.UsedSizeBytes
	snapshot.Heat.ChunkDB.FreeBytes = inventory.ChunkDB.Storage.FreeSizeBytes
	snapshot.Heat.ChunkDB.TotalBytes = inventory.ChunkDB.Storage.TotalSizeBytes
	snapshot.Heat.ChunkDB.UsagePercent = usagePercent
}
