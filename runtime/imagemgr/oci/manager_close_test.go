package oci

import (
	"path/filepath"
	"testing"
)

func TestClosePreservesMountsForLeaseOwnedRecovery(t *testing.T) {
	mgr := newTestManager(t)
	ref := "example.local/restart:dev"
	if _, err := mgr.ImportImageArchive(t.Context(), ref, writeMountableDockerArchive(t, ref)); err != nil {
		t.Fatal(err)
	}
	mounted, err := mgr.MountImageWithContext(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	key, imported, err := mgr.ResolveImportedImageCacheKey(ref)
	if err != nil || !imported {
		t.Fatalf("resolve imported key: imported=%v err=%v", imported, err)
	}
	mgr.unmountFn = func(path string) error {
		t.Errorf("process shutdown must not release a lease-owned mount: %s", path)
		return nil
	}
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := openMetadataStore(filepath.Join(mgr.root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	mgr.store = reopened
	mgr.containers = nil
	mgr.readMnts = func() (managedMountSnapshot, error) {
		return managedMountSnapshot{paths: map[string]struct{}{mounted.MountPath: {}}, authoritative: true}, nil
	}
	if err := mgr.reconcileState(); err != nil {
		t.Fatal(err)
	}
	projection, err := reopened.getMount(key)
	if err != nil || projection == nil || projection.MountPath != mounted.MountPath {
		t.Fatalf("mount projection lost across close/recovery: %+v, %v", projection, err)
	}
	if recovered := mgr.getContainer(key); recovered == nil || recovered.MountPath != mounted.MountPath {
		t.Fatalf("mount not recovered: %+v", recovered)
	}
	for _, digest := range projection.LayerDigests {
		layer, err := reopened.getLayer(digest)
		if err != nil || layer == nil || layer.RefCount != 1 {
			t.Fatalf("live layer reference lost: %+v, %v", layer, err)
		}
	}
	for _, id := range projection.ChainIDs {
		chain, err := reopened.getChain(id)
		if err != nil || chain == nil || chain.RefCount != 1 {
			t.Fatalf("live chain reference lost: %+v, %v", chain, err)
		}
	}
	if record, err := reopened.getImport(ref); err != nil || record == nil {
		t.Fatalf("import lost: %+v, %v", record, err)
	}
}
