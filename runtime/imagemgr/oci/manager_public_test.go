package oci

import (
	"path/filepath"
	"testing"
)

func TestListMountedImageURLs(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:     "docker.io/library/z:latest",
		MountID:      "m1",
		MountPath:    filepath.Join(mgr.mountsDir, "m1", "merged"),
		LayerDigests: []string{"sha256:a"},
	}); err != nil {
		t.Fatalf("put mount m1: %v", err)
	}
	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:     "docker.io/library/a:latest",
		MountID:      "m2",
		MountPath:    filepath.Join(mgr.mountsDir, "m2", "merged"),
		LayerDigests: []string{"sha256:b"},
	}); err != nil {
		t.Fatalf("put mount m2: %v", err)
	}

	got, err := mgr.ListMountedImageURLs()
	if err != nil {
		t.Fatalf("ListMountedImageURLs() error: %v", err)
	}

	want := []string{
		"docker.io/library/a:latest",
		"docker.io/library/z:latest",
	}
	if len(got) != len(want) {
		t.Fatalf("ListMountedImageURLs() length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListMountedImageURLs()[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestListMountedDetails(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:      "docker.io/library/z:latest",
		MountID:       "m1",
		MountPath:     filepath.Join(mgr.mountsDir, "m1", "merged"),
		LayerDigests:  []string{"sha256:z1", "sha256:z2"},
		ChainIDs:      []string{"sha256:cz1", "sha256:cz2"},
		LowerDirs:     []string{"/layers/lz1", "/layers/lz2"},
		CreatedAtUnix: 100,
	}); err != nil {
		t.Fatalf("put mount m1: %v", err)
	}
	if err := mgr.store.putMount(&OciMountRecord{
		ImageURL:      "docker.io/library/a:latest",
		MountID:       "m2",
		MountPath:     filepath.Join(mgr.mountsDir, "m2", "merged"),
		LayerDigests:  []string{"sha256:a1"},
		ChainIDs:      []string{"sha256:ca1"},
		LowerDirs:     []string{"/layers/la1"},
		CreatedAtUnix: 200,
	}); err != nil {
		t.Fatalf("put mount m2: %v", err)
	}

	got, err := mgr.ListMountedDetails()
	if err != nil {
		t.Fatalf("ListMountedDetails() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListMountedDetails() length = %d, want %d", len(got), 2)
	}
	if got[0].ImageURL != "docker.io/library/a:latest" || got[1].ImageURL != "docker.io/library/z:latest" {
		t.Fatalf("ListMountedDetails() sorting unexpected: %+v", got)
	}
	if got[0].MountID != "m2" || got[0].MountPath == "" {
		t.Fatalf("ListMountedDetails()[0] invalid detail: %+v", got[0])
	}

	got[1].LayerDigests[0] = "mutated"
	got[1].ChainIDs[0] = "mutated"
	got[1].LowerDirs[0] = "mutated"
	again, err := mgr.ListMountedDetails()
	if err != nil {
		t.Fatalf("ListMountedDetails() second call error: %v", err)
	}
	if again[1].LayerDigests[0] == "mutated" || again[1].ChainIDs[0] == "mutated" || again[1].LowerDirs[0] == "mutated" {
		t.Fatalf("ListMountedDetails() should return deep-copied slices")
	}
}
