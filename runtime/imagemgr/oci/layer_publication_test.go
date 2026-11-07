package oci

import (
	"archive/tar"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

type callbackMetadataLayer struct {
	v1.Layer
	beforeRead func()
}

func (layer callbackMetadataLayer) Uncompressed() (io.ReadCloser, error) {
	layer.beforeRead()
	return layer.Layer.Uncompressed()
}

func TestLayerPublicationCommitsFactsBeforeCanonicalContent(t *testing.T) {
	for _, afterRename := range []bool{false, true} {
		name := "before rename"
		if afterRename {
			name = "after rename before reservation"
		}
		t.Run(name, func(t *testing.T) {
			mgr := newTestManager(t)
			defer mgr.store.close()
			layer := metadataTestLayer(t, &tar.Header{Name: "parent/file", Typeflag: tar.TypeReg, Mode: 0644})
			digest, err := layer.Digest()
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("interrupted publication")
			mgr.renameLayerFn = func(source, target string) error {
				facts, err := mgr.store.getLayer(digest.String())
				if err != nil || facts == nil || facts.RefCount != 0 || facts.RefZeroAtUnix == 0 ||
					len(facts.ImplicitDirectories) != 1 || facts.ImplicitDirectories[0] != "parent" {
					t.Fatalf("canonical publication preceded complete facts: %+v, %v", facts, err)
				}
				if afterRename {
					if err := os.Rename(source, target); err != nil {
						t.Fatal(err)
					}
				}
				return injected
			}
			if _, err := mgr.ensureLayerExtracted(layer); !errors.Is(err, injected) {
				t.Fatalf("publication error = %v", err)
			}
			facts, err := mgr.store.getLayer(digest.String())
			if err != nil || facts == nil || facts.RefCount != 0 || facts.RefZeroAtUnix == 0 {
				t.Fatalf("interrupted facts = %+v, %v", facts, err)
			}
			if pathExists(facts.Path) != afterRename {
				t.Fatal("unexpected canonical publication boundary")
			}
			mgr.renameLayerFn = os.Rename
			var retry v1.Layer = layer
			if afterRename {
				retry = panicUncompressedLayer{digest: digest}
			}
			got, err := mgr.ensureLayerExtracted(retry)
			if err != nil || got.RefCount != 1 || got.RefZeroAtUnix != 0 {
				t.Fatalf("explicit recovery = %+v, %v", got, err)
			}
		})
	}
}

func TestLayerFactsWriteFailureNeverPublishesCanonicalContent(t *testing.T) {
	mgr := newTestManager(t)
	base := metadataTestLayer(t, &tar.Header{Name: "input", Typeflag: tar.TypeReg, Mode: 0644})
	digest, err := base.Digest()
	if err != nil {
		t.Fatal(err)
	}
	renameCalled := false
	mgr.renameLayerFn = func(string, string) error { renameCalled = true; return nil }
	layer := callbackMetadataLayer{Layer: base, beforeRead: func() {
		if err := mgr.store.close(); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := mgr.ensureLayerExtracted(layer); err == nil {
		t.Fatal("closed metadata store accepted publication")
	}
	if renameCalled {
		t.Fatal("canonical content was published before its facts committed")
	}
	mgr.store, err = openMetadataStore(filepath.Join(mgr.root, "metadata.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.store.close()
	if record, err := mgr.store.getLayer(digest.String()); err != nil || record != nil {
		t.Fatalf("failed commit published a record: %+v, %v", record, err)
	}
}

func TestMissingReferencedLayerDoesNotResetHeldReferences(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	layer := panicUncompressedLayer{digest: v1.Hash{Algorithm: "sha256", Hex: "held-reference"}}
	dir, err := mgr.store.getOrCreateLayerDir(layer.digest.String())
	if err != nil {
		t.Fatal(err)
	}
	before := &LayerRecord{Digest: layer.digest.String(), Path: filepath.Join(mgr.layersDir, dir, "fs"),
		RefCount: 2, ImplicitDirectories: []string{}}
	if err := mgr.store.putLayer(before); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ensureLayerExtracted(layer); err == nil {
		t.Fatal("missing referenced content was silently replaced")
	}
	after, err := mgr.store.getLayer(layer.digest.String())
	if err != nil || after.RefCount != 2 || after.RefZeroAtUnix != 0 || pathExists(before.Path) {
		t.Fatalf("held obligation changed: %+v, %v", after, err)
	}
}

func TestLayerPublicationAndGCShareTheReservationBarrier(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	layer := metadataTestLayer(t, &tar.Header{Name: "input", Typeflag: tar.TypeReg, Mode: 0644})
	digest, err := layer.Digest()
	if err != nil {
		t.Fatal(err)
	}
	entered, release, published := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	mgr.renameLayerFn = func(source, target string) error {
		close(entered)
		<-release
		return os.Rename(source, target)
	}
	var publishError error
	go func() {
		defer close(published)
		_, publishError = mgr.ensureLayerExtracted(layer)
	}()
	defer func() { unblock(); <-published }()
	select {
	case <-entered:
	case <-published:
		t.Fatalf("publisher exited before its barrier: %v", publishError)
	case <-time.After(5 * time.Second):
		t.Fatal("publisher did not reach its barrier")
	}
	facts, err := mgr.store.getLayer(digest.String())
	if err != nil {
		t.Fatal(err)
	}
	gcDone := make(chan struct{})
	var removed bool
	var gcError error
	go func() {
		defer close(gcDone)
		removed, gcError = mgr.tryDeleteLayerCandidate(facts, mgr.now(), 0, gcIgnoreAge, "publication test")
	}()
	defer func() { unblock(); <-gcDone }()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for {
		mgr.mutex.Lock()
		entry := mgr.layerLocks[digest.String()]
		queued := entry != nil && entry.refs == 2
		mgr.mutex.Unlock()
		if queued {
			break
		}
		select {
		case <-gcDone:
			t.Fatal("GC did not wait on the publisher's ownership lock")
		case <-deadline:
			t.Fatal("GC did not enter the shared ownership lock")
		case <-ticker.C:
		}
	}
	unblock()
	<-published
	<-gcDone
	if publishError != nil || gcError != nil || removed || !pathExists(facts.Path) {
		t.Fatalf("publication=%v GC=%v removed=%v", publishError, gcError, removed)
	}
}
