package nodeinventory

import (
	"context"
	"sort"
	"time"
)

func (s *AxnodedSource) collectImagemgrInventory(now time.Time, snapshot *NodeInventorySnapshot) {
	if s.imageManager == nil || !s.imageManager.Enabled() {
		snapshot.Sources["imagemgr"] = SourceStatus{Status: StatusDisabled}
		snapshot.Components.Imagemgr.Status = StatusDisabled
		return
	}
	inventory, err := s.imageManager.Inventory(context.Background())
	if err != nil {
		snapshot.Sources["imagemgr"] = errorSource(err)
		snapshot.Components.Imagemgr.Status = StatusError
		snapshot.Components.Imagemgr.Error = err.Error()
		return
	}
	snapshot.Sources["imagemgr"] = readySource(now)
	snapshot.Components.Imagemgr.Status = StatusReady
	snapshot.Components.Imagemgr.Reachable = true
	snapshot.Components.Imagemgr.MountedImageCount = len(inventory.MountedImages)
	snapshot.Components.Imagemgr.ImportedImageCount = len(inventory.ImportedImages)
	mergeImagemgrLocality(snapshot, inventory)
	mountedURLs := make([]string, 0, len(inventory.MountedImages))
	for _, mount := range inventory.MountedImages {
		mountedURLs = append(mountedURLs, mount.ImageURL)
	}
	sort.Strings(mountedURLs)
	snapshot.Heat.MountedImageURLs = mountedURLs
	snapshot.Heat.MountedRootfsCount = len(inventory.MountedImages)
}
