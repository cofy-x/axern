package natbench

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
)

type LocalitySummary struct {
	Key    string                            `json:"key,omitempty"`
	Entry  *nodeinventory.LocalityHeatEntry  `json:"entry,omitempty"`
	Ranked []nodeinventory.LocalityHeatEntry `json:"ranked,omitempty"`
}

func LocalRootfsKey(path string) string {
	return "local:" + filepath.Clean(path)
}

func ImageRootfsKey(imageURL string) string {
	return "image:" + strings.TrimSpace(imageURL)
}

func S3RootfsKey(endpoint, bucket, object string) string {
	endpoint = strings.TrimSpace(endpoint)
	bucket = strings.TrimSpace(bucket)
	object = strings.TrimSpace(object)
	return "s3:" + endpoint + "/" + bucket + "/" + object
}

func CaptureLocalitySummary(inventoryURL, key string) (*LocalitySummary, error) {
	if inventoryURL == "" {
		return nil, nil
	}

	resp, err := http.Get(inventoryURL)
	if err != nil {
		return nil, fmt.Errorf("fetch node inventory: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch node inventory: unexpected status %d", resp.StatusCode)
	}

	var snapshot nodeinventory.NodeInventorySnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		return nil, fmt.Errorf("decode node inventory: %w", err)
	}
	if len(snapshot.Heat.Locality) == 0 {
		return nil, nil
	}

	ranked := RankLocalityEntries(snapshot.Heat.Locality)
	summary := &LocalitySummary{
		Key:    key,
		Ranked: ranked,
	}
	if key != "" {
		for idx := range ranked {
			if ranked[idx].Key == key {
				entry := ranked[idx]
				summary.Entry = &entry
				break
			}
		}
	}
	return summary, nil
}

func RankLocalityEntries(entries []nodeinventory.LocalityHeatEntry) []nodeinventory.LocalityHeatEntry {
	ranked := slices.Clone(entries)
	sort.Slice(ranked, func(i, j int) bool {
		a, b := ranked[i], ranked[j]
		switch {
		case a.Mounted != b.Mounted:
			return a.Mounted
		case a.RetainedRootfsCount != b.RetainedRootfsCount:
			return a.RetainedRootfsCount > b.RetainedRootfsCount
		case a.RetainedRuntimeCount != b.RetainedRuntimeCount:
			return a.RetainedRuntimeCount > b.RetainedRuntimeCount
		case a.NydusDaemonAlive != b.NydusDaemonAlive:
			return a.NydusDaemonAlive
		case localityAgeSortValue(a.ChunkDBRecentAccessAgeSecs) != localityAgeSortValue(b.ChunkDBRecentAccessAgeSecs):
			return localityAgeSortValue(a.ChunkDBRecentAccessAgeSecs) < localityAgeSortValue(b.ChunkDBRecentAccessAgeSecs)
		case a.PeerHealthyCount != b.PeerHealthyCount:
			return a.PeerHealthyCount > b.PeerHealthyCount
		case a.PeerHintedCount != b.PeerHintedCount:
			return a.PeerHintedCount > b.PeerHintedCount
		default:
			return a.Key < b.Key
		}
	})
	return ranked
}

func localityAgeSortValue(age int64) int64 {
	if age <= 0 {
		return int64(^uint64(0) >> 1)
	}
	return age
}
