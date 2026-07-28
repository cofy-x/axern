package nodeinventory

import (
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
)

func (s *AxnodedSource) collectAxnodedLocality(snapshot *NodeInventorySnapshot, runningContainers []*container.Container) {
	rootfsSeen := make(map[*langruntime.RootFS]struct{})
	for _, lr := range s.langRuntime.List() {
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
			entry.RetainedRuntimeCount++
		}
		if lr.RootFS.RetainedRefCount() > 0 {
			if _, ok := rootfsSeen[lr.RootFS]; !ok {
				rootfsSeen[lr.RootFS] = struct{}{}
				entry.RetainedRootfsCount++
			}
		}
	}

	for _, c := range runningContainers {
		if c == nil || c.Metadata == nil || c.Metadata.Labels == nil {
			continue
		}
		runtimeID := c.Metadata.Labels[workloadidentity.LabelKeyRuntimeID]
		if runtimeID == "" {
			continue
		}
		lr := s.langRuntime.GetLangRuntime(runtimeID)
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

func mergeImagemgrLocality(snapshot *NodeInventorySnapshot, locality []ImageLocalityEntry) {
	for _, imageLocality := range locality {
		entry := ensureLocalityEntry(
			&snapshot.Heat.Locality,
			imageLocality.Key,
			rootfsTypeFromLocalityKey(imageLocality.Key),
			imageLocality.MountType,
		)
		if imageLocality.MountType != "" {
			entry.MountType = imageLocality.MountType
		}
		entry.Mounted = entry.Mounted || imageLocality.Mounted
		entry.NydusDaemonAlive = entry.NydusDaemonAlive || imageLocality.DaemonAlive && imageLocality.MountType == "nydus"
		entry.ChunkDBTotalChunks = imageLocality.ChunkDBTotalChunks
		entry.ChunkDBUsedBytes = imageLocality.ChunkDBUsedBytes
		entry.ChunkDBRecentAccessAgeSecs = imageLocality.ChunkDBRecentAccessAgeSecs
		entry.PeerHealthyCount = imageLocality.PeerHealthyCount
		entry.PeerUnhealthyCount = imageLocality.PeerUnhealthyCount
		entry.PeerHintedCount = imageLocality.PeerHintedCount
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

func rootfsTypeFromLocalityKey(key string) string {
	switch {
	case strings.HasPrefix(key, "local:"):
		return "local"
	case strings.HasPrefix(key, "image:"):
		return "image"
	case strings.HasPrefix(key, "s3:"):
		return "s3"
	default:
		return "unknown"
	}
}

func sortLocalityEntries(entries []LocalityHeatEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})
}
