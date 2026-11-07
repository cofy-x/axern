package oci

import (
	"archive/tar"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectExtractionReportsRequiredOverlayNamespace(t *testing.T) {
	for _, name := range []string{".wh..wh..opq", ".wh.removed"} {
		t.Run(name, func(t *testing.T) {
			layer := metadataTestLayer(t, &tar.Header{Name: name, Typeflag: tar.TypeReg})
			stream, err := layer.Uncompressed()
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			root := t.TempDir()
			extracted, err := extractLayerTar(stream, root)
			if err != nil {
				t.Fatal(err)
			}
			path, attr := root, "opaque"
			if name != ".wh..wh..opq" {
				path, attr = filepath.Join(root, "removed"), "whiteout"
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if isDeviceWhiteout(info) {
					if extracted.RequiresUserXattr {
						t.Fatal("device whiteout incorrectly requires userxattr")
					}
					return
				}
			}
			_, qualified, err := overlayAttribute(path, attr)
			if err != nil || extracted.RequiresUserXattr == qualified {
				t.Fatalf("reported namespace requirement=%v qualified=%v err=%v", extracted.RequiresUserXattr, qualified, err)
			}
		})
	}
}

func TestProductionPublisherRejectsUserOnlyOpaqueWithoutImplicitPaths(t *testing.T) {
	layer := metadataTestLayer(t, &tar.Header{Name: ".wh..wh..opq", Typeflag: tar.TypeReg})
	stream, err := layer.Uncompressed()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	facts, err := extractLayerTar(stream, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !facts.RequiresUserXattr {
		t.Skip("host provides trusted overlay metadata; user-only fallback is unavailable")
	}
	if len(facts.ImplicitDirectories) != 0 {
		t.Fatal("fixture must exercise the empty implicit-directory case")
	}
	mgr := newTestManager(t)
	defer mgr.store.close()
	if _, err := mgr.ensureLayerExtracted(layer); err == nil || !strings.Contains(err.Error(), "requires userxattr") {
		t.Fatalf("unqualified representation was not rejected: %v", err)
	}
	digest, err := layer.Digest()
	if err != nil {
		t.Fatal(err)
	}
	record, err := mgr.store.getLayer(digest.String())
	if err != nil || record != nil {
		t.Fatalf("unqualified facts were committed: %+v %v", record, err)
	}
	dir, err := mgr.store.getOrCreateLayerDir(digest.String())
	if err != nil {
		t.Fatal(err)
	}
	if pathExists(filepath.Join(mgr.layersDir, dir, "fs")) {
		t.Fatal("unqualified canonical content was published")
	}
}

func TestOverlayMetadataFailureIsAnError(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := setOverlayXattr(file, "opaque", []byte("y")); err == nil {
		t.Fatal("failure of both namespaces was silently accepted")
	}
}
