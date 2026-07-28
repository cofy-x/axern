package nodeinventory

import "testing"

func TestMergeImagemgrLocalityPreservesAxnodedCounts(t *testing.T) {
	snapshot := NewSnapshot()
	snapshot.Heat.Locality = []LocalityHeatEntry{{
		Key:                   "image:docker.io/library/nginx:latest",
		RootfsType:            "image",
		MountType:             "oci",
		RetainedRuntimeCount:  2,
		RetainedRootfsCount:   1,
		RunningContainerCount: 3,
	}}

	mergeImagemgrLocality(&snapshot, []ImageLocalityEntry{{
		Key:                        "image:docker.io/library/nginx:latest",
		MountType:                  "nydus",
		Mounted:                    true,
		DaemonAlive:                true,
		ChunkDBTotalChunks:         5,
		ChunkDBUsedBytes:           128,
		ChunkDBRecentAccessAgeSecs: 7,
		PeerHealthyCount:           2,
		PeerUnhealthyCount:         1,
		PeerHintedCount:            4,
	}})

	if len(snapshot.Heat.Locality) != 1 {
		t.Fatalf("locality entries = %d, want 1", len(snapshot.Heat.Locality))
	}
	entry := snapshot.Heat.Locality[0]
	if entry.MountType != "nydus" {
		t.Fatalf("mount_type = %q, want nydus", entry.MountType)
	}
	if !entry.Mounted || !entry.NydusDaemonAlive {
		t.Fatalf("merged mounted/daemon flags = %+v", entry)
	}
	if entry.RetainedRuntimeCount != 2 || entry.RetainedRootfsCount != 1 || entry.RunningContainerCount != 3 {
		t.Fatalf("axnoded counts lost after merge: %+v", entry)
	}
	if entry.PeerHintedCount != 4 || entry.ChunkDBTotalChunks != 5 {
		t.Fatalf("imagemgr locality fields not merged: %+v", entry)
	}
}
