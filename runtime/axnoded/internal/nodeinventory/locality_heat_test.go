package nodeinventory

import "testing"

func TestMergeOCIInventoryPreservesRetainedExecutionAndCacheIdentity(t *testing.T) {
	snapshot := NewSnapshot()
	snapshot.Heat.Locality = []LocalityHeatEntry{{Key: "image:opaque-owned-cache", RootfsType: "image", MountType: "oci", RetainedEnvironmentCount: 2, RetainedRootfsCount: 1, RunningContainerCount: 3}}
	mergeImagemgrLocality(&snapshot, &ImageManagerInventory{
		MountedImages:  []MountedImageDetail{{CacheKey: "opaque-owned-cache", ImageURL: "registry.example/project@sha256:frozen", MountType: "oci", MountPath: "/owned/mount"}},
		ImportedImages: []ImportedImageDetail{{CacheKey: "opaque-import-cache", ImageRef: "registry.example/import:tag"}},
	})
	if len(snapshot.Heat.Locality) != 2 {
		t.Fatalf("locality entries = %d, want exact mounted and imported identities", len(snapshot.Heat.Locality))
	}
	entry := snapshot.Heat.Locality[0]
	if !entry.Mounted || entry.MountType != "oci" || entry.RetainedEnvironmentCount != 2 || entry.RetainedRootfsCount != 1 || entry.RunningContainerCount != 3 {
		t.Fatalf("retained execution facts lost after OCI inventory merge: %+v", entry)
	}
	if imported := snapshot.Heat.Locality[1]; imported.Key != "image:opaque-import-cache" || imported.MountType != "oci" || imported.Mounted {
		t.Fatalf("imported content must retain the source-owned key without claiming a live mount: %+v", imported)
	}
}
