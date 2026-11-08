package nodeinventory

import (
	"sort"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
)

func (s *AxnodedSource) collectAxnodedLocality(snapshot *NodeInventorySnapshot, runningContainers []*container.Container) {
	rootfsSeen := make(map[*environmentcache.RootFS]struct{})
	for _, lr := range s.preparedEnvironment.List() {
		if lr == nil || lr.RootFS == nil {
			continue
		}
		key, ok := LocalityKeyFromRootfsConfig(lr.RootFS.Config())
		if !ok {
			continue
		}
		entry := ensureLocalityEntry(
			&snapshot.Heat.Locality,
			key,
			RootfsTypeFromConfig(lr.RootFS.Config()),
			MountTypeFromConfig(lr.RootFS.Config()),
		)
		entry.Mounted = entry.Mounted || lr.RootFS.Path() != ""
		if lr.Retained() {
			entry.RetainedEnvironmentCount++
		}
		if lr.RootFS.RetainedRefCount() > 0 {
			if _, ok := rootfsSeen[lr.RootFS]; !ok {
				rootfsSeen[lr.RootFS] = struct{}{}
				entry.RetainedRootfsCount++
			}
		}
	}

	for _, c := range runningContainers {
		if c == nil || s.allocationEnvironmentID == nil {
			continue
		}
		runtimeID := s.allocationEnvironmentID(c.ID)
		if runtimeID == "" {
			continue
		}
		lr := s.preparedEnvironment.GetPreparedEnvironment(runtimeID)
		if lr == nil || lr.RootFS == nil {
			continue
		}
		key, ok := LocalityKeyFromRootfsConfig(lr.RootFS.Config())
		if !ok {
			continue
		}
		entry := ensureLocalityEntry(
			&snapshot.Heat.Locality,
			key,
			RootfsTypeFromConfig(lr.RootFS.Config()),
			MountTypeFromConfig(lr.RootFS.Config()),
		)
		entry.Mounted = entry.Mounted || lr.RootFS.Path() != ""
		entry.RunningContainerCount++
	}
}

// Merge source-owned OCI cache identities with retained execution facts.
func mergeImagemgrLocality(snapshot *NodeInventorySnapshot, inventory *ImageManagerInventory) {
	for _, imported := range inventory.ImportedImages {
		key := imported.CacheKey
		if key == "" {
			key = imported.ImageRef
		}
		if key != "" {
			ensureLocalityEntry(&snapshot.Heat.Locality, "image:"+key, "image", "oci")
		}
	}
	for _, mounted := range inventory.MountedImages {
		key := mounted.CacheKey
		if key == "" {
			key = mounted.ImageURL
		}
		if key == "" || mounted.MountType != "oci" {
			continue
		}
		entry := ensureLocalityEntry(&snapshot.Heat.Locality, "image:"+key, "image", "oci")
		entry.Mounted = true
	}
}

func ensureLocalityEntry(entries *[]LocalityHeatEntry, key, rootfsType, mountType string) *LocalityHeatEntry {
	for idx := range *entries {
		if (*entries)[idx].Key == key {
			if (*entries)[idx].RootfsType == "" {
				(*entries)[idx].RootfsType = rootfsType
			}
			if (*entries)[idx].MountType == "" {
				(*entries)[idx].MountType = mountType
			}
			return &(*entries)[idx]
		}
	}
	*entries = append(*entries, LocalityHeatEntry{
		Key:        key,
		RootfsType: rootfsType,
		MountType:  mountType,
	})
	return &(*entries)[len(*entries)-1]
}

func sortLocalityEntries(entries []LocalityHeatEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
}
