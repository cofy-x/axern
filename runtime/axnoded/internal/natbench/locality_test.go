package natbench

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
)

func TestRankLocalityEntries(t *testing.T) {
	ranked := RankLocalityEntries([]nodeinventory.LocalityHeatEntry{
		{
			Key:                  "image:cold",
			Mounted:              false,
			RetainedRootfsCount:  0,
			RetainedRuntimeCount: 0,
		},
		{
			Key:                        "image:warm",
			Mounted:                    true,
			RetainedRootfsCount:        1,
			RetainedRuntimeCount:       1,
			ChunkDBRecentAccessAgeSecs: 9,
			PeerHealthyCount:           1,
		},
		{
			Key:                        "image:hotter",
			Mounted:                    true,
			RetainedRootfsCount:        1,
			RetainedRuntimeCount:       1,
			NydusDaemonAlive:           true,
			ChunkDBRecentAccessAgeSecs: 5,
			PeerHealthyCount:           2,
			PeerHintedCount:            4,
		},
	})

	if len(ranked) != 3 {
		t.Fatalf("ranked length = %d, want 3", len(ranked))
	}
	if ranked[0].Key != "image:hotter" {
		t.Fatalf("first ranked key = %q, want image:hotter", ranked[0].Key)
	}
	if ranked[2].Key != "image:cold" {
		t.Fatalf("last ranked key = %q, want image:cold", ranked[2].Key)
	}
}
