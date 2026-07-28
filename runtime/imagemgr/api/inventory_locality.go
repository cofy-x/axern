package api

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
)

func (w *HttpWorker) buildLocalityEntries(
	mounts []MountedImageDetail,
	daemons []imagefsd.DaemonInfo,
) ([]ImageLocalityEntry, string) {
	daemonByMount := make(map[string]imagefsd.DaemonInfo, len(daemons))
	hasDaemonBackedEntry := false
	for _, daemon := range daemons {
		daemonByMount[daemon.MountPoint] = daemon
		if daemon.SourceType == imagefsd.SourceTypeNydus || daemon.SourceType == imagefsd.SourceTypeOSS {
			hasDaemonBackedEntry = true
		}
	}

	var (
		stats       *imagefsd.LocalityStats
		localityErr string
	)
	if hasDaemonBackedEntry {
		var err error
		stats, err = w.mgr.LocalityStats()
		if err != nil {
			localityErr = err.Error()
		}
	}

	entries := make(map[string]*ImageLocalityEntry)
	for _, mount := range mounts {
		key, ok := mountLocalityKey(mount)
		if !ok {
			continue
		}
		entry := &ImageLocalityEntry{
			Key:           key,
			MountType:     mount.MountType,
			ImageURL:      mount.ImageURL,
			NydusImageURL: mount.NydusImageURL,
			MountPath:     mount.MountPath,
			Mounted:       true,
		}
		if mount.MountType == MountTypeNydus {
			if daemon, ok := daemonByMount[mount.MountPath]; ok {
				mergeDaemonLocality(entry, daemon, stats)
			}
		}
		entries[key] = entry
	}

	for _, daemon := range daemons {
		key, mountType, ok := daemonLocalityKey(daemon)
		if !ok {
			continue
		}
		entry, exists := entries[key]
		if !exists {
			entry = &ImageLocalityEntry{
				Key:       key,
				MountType: mountType,
				MountPath: daemon.MountPoint,
				Mounted:   daemon.IsAlive,
			}
			if daemon.SourceType == imagefsd.SourceTypeNydus {
				entry.ImageURL = daemon.ImageURL
				entry.NydusImageURL = daemon.ImageURL
			}
			entries[key] = entry
		}
		mergeDaemonLocality(entry, daemon, stats)
	}

	ordered := make([]ImageLocalityEntry, 0, len(entries))
	for _, entry := range entries {
		ordered = append(ordered, *entry)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Key < ordered[j].Key
	})
	return ordered, localityErr
}

func mountLocalityKey(mount MountedImageDetail) (string, bool) {
	switch mount.MountType {
	case MountTypeOCI:
		if mount.ImageURL == "" {
			return "", false
		}
		return "image:" + mount.ImageURL, true
	case MountTypeNydus:
		imageURL := mount.NydusImageURL
		if imageURL == "" {
			imageURL = mount.ImageURL
		}
		if imageURL == "" {
			return "", false
		}
		return "image:" + imageURL, true
	default:
		return "", false
	}
}

func daemonLocalityKey(daemon imagefsd.DaemonInfo) (string, MountType, bool) {
	switch daemon.SourceType {
	case imagefsd.SourceTypeNydus:
		if daemon.ImageURL == "" {
			return "", "", false
		}
		return "image:" + daemon.ImageURL, MountTypeNydus, true
	case imagefsd.SourceTypeOSS:
		object := strings.TrimPrefix(daemon.ObjectPrefix+daemon.Name, "/")
		if daemon.Endpoint == "" || daemon.Bucket == "" || object == "" {
			return "", "", false
		}
		return fmt.Sprintf("s3:%s/%s/%s", daemon.Endpoint, daemon.Bucket, object), MountTypeOSS, true
	default:
		return "", "", false
	}
}

func mergeDaemonLocality(
	entry *ImageLocalityEntry,
	daemon imagefsd.DaemonInfo,
	stats *imagefsd.LocalityStats,
) {
	entry.DaemonID = daemon.ID
	entry.DaemonAlive = daemon.IsAlive
	if entry.MountPath == "" {
		entry.MountPath = daemon.MountPoint
	}
	if entry.ImageURL == "" && daemon.ImageURL != "" {
		entry.ImageURL = daemon.ImageURL
	}
	if entry.NydusImageURL == "" && daemon.SourceType == imagefsd.SourceTypeNydus {
		entry.NydusImageURL = daemon.ImageURL
	}
	if stats == nil || !daemon.IsAlive {
		return
	}
	entry.ChunkDBTotalChunks = stats.ChunkDBTotalChunks
	entry.ChunkDBUsedBytes = stats.ChunkDBUsedBytes
	entry.ChunkDBRecentAccessAgeSecs = stats.ChunkDBRecentAccessAgeSec
	entry.PeerHealthyCount = stats.PeerHealthyCount
	entry.PeerUnhealthyCount = stats.PeerUnhealthyCount
	entry.PeerHintedCount = stats.PeerHintedCount
}
