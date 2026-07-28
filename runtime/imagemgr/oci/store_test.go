package oci

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestMetadataStore_LayerRefCountAtomicUpdates(t *testing.T) {
	store, err := openMetadataStore(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("openMetadataStore() error: %v", err)
	}
	defer store.close()

	if err := store.putLayer(&LayerRecord{
		Digest:       "sha256:test",
		Path:         "/tmp/layer",
		RefCount:     0,
		LastUsedUnix: 1,
	}); err != nil {
		t.Fatalf("putLayer() error: %v", err)
	}

	const workers = 64
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.incrementLayerRef("sha256:test", 2); err != nil {
				t.Errorf("incrementLayerRef() error: %v", err)
			}
		}()
	}
	wg.Wait()

	rec, err := store.getLayer("sha256:test")
	if err != nil {
		t.Fatalf("getLayer() error: %v", err)
	}
	if rec.RefCount != workers {
		t.Fatalf("expected refcount=%d, got %d", workers, rec.RefCount)
	}
	if rec.RefZeroAtUnix != 0 {
		t.Fatalf("expected RefZeroAtUnix=0 while referenced, got %d", rec.RefZeroAtUnix)
	}

	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			if _, err := store.decrementLayerRef("sha256:test", 3); err != nil {
				t.Errorf("decrementLayerRef() error: %v", err)
			}
		}()
	}
	wg.Wait()

	rec, err = store.getLayer("sha256:test")
	if err != nil {
		t.Fatalf("getLayer() error: %v", err)
	}
	if rec.RefCount != 0 {
		t.Fatalf("expected refcount=0, got %d", rec.RefCount)
	}
	if rec.RefZeroAtUnix != 3 {
		t.Fatalf("expected RefZeroAtUnix=3 after reaching zero, got %d", rec.RefZeroAtUnix)
	}
}

func TestMetadataStore_LayerRefCountNotFound(t *testing.T) {
	store, err := openMetadataStore(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("openMetadataStore() error: %v", err)
	}
	defer store.close()

	if _, err := store.incrementLayerRef("sha256:missing", 1); !errors.Is(err, ErrLayerNotFound) {
		t.Fatalf("expected ErrLayerNotFound for increment, got %v", err)
	}
	if _, err := store.decrementLayerRef("sha256:missing", 1); !errors.Is(err, ErrLayerNotFound) {
		t.Fatalf("expected ErrLayerNotFound for decrement, got %v", err)
	}
}

func TestMetadataStore_GetOrCreateLayerDir(t *testing.T) {
	store, err := openMetadataStore(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("openMetadataStore() error: %v", err)
	}
	defer store.close()

	dir1, err := store.getOrCreateLayerDir("sha256:aaa")
	if err != nil {
		t.Fatalf("getOrCreateLayerDir() error: %v", err)
	}
	dir1Again, err := store.getOrCreateLayerDir("sha256:aaa")
	if err != nil {
		t.Fatalf("getOrCreateLayerDir() error: %v", err)
	}
	if dir1 != dir1Again {
		t.Fatalf("expected stable layer dir mapping, got %s and %s", dir1, dir1Again)
	}

	dir2, err := store.getOrCreateLayerDir("sha256:bbb")
	if err != nil {
		t.Fatalf("getOrCreateLayerDir() error: %v", err)
	}
	if dir1 == dir2 {
		t.Fatalf("different digests should map to different dirs")
	}
	if !strings.HasPrefix(dir1, "l") || !strings.HasPrefix(dir2, "l") {
		t.Fatalf("expected compact layer dir names, got %s and %s", dir1, dir2)
	}

	got, err := store.getLayerDir("sha256:bbb")
	if err != nil {
		t.Fatalf("getLayerDir() error: %v", err)
	}
	if got != dir2 {
		t.Fatalf("expected getLayerDir=%s, got %s", dir2, got)
	}
}
