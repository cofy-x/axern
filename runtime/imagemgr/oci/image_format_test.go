package oci

import (
	"archive/tar"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/imageregistry"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestMountRejectsNydusBeforeFetchingLayerContent(t *testing.T) {
	for _, marker := range []string{"media-type", "layer-bootstrap", "layer-blob", "manifest-bootstrap", "manifest-blob", "inline-auth"} {
		t.Run(marker, func(t *testing.T) {
			var blobReads atomic.Int32
			registryHandler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if marker == "inline-auth" {
					user, password, _ := r.BasicAuth()
					if user != "agent" || password != "secret" {
						w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
				}
				if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") {
					blobReads.Add(1)
				}
				registryHandler.ServeHTTP(w, r)
			}))
			defer server.Close()
			host := strings.TrimPrefix(server.URL, "http://")
			t.Setenv("IMAGEMGR_INSECURE_REGISTRIES", host)
			ref, err := name.NewTag(host+"/image:test", name.Insecure)
			if err != nil {
				t.Fatal(err)
			}
			layer := static.NewLayer(formatLayerTar(t, "hello", []byte("ordinary data")), types.OCIUncompressedLayer)
			addendum := mutate.Addendum{Layer: layer, MediaType: types.OCIUncompressedLayer}
			switch marker {
			case "media-type":
				addendum.MediaType = types.MediaType(retiredMediaTypeNydusBlob)
			case "layer-bootstrap", "inline-auth":
				addendum.Annotations = map[string]string{retiredLayerAnnotationNydusBootstrap: "true"}
			case "layer-blob":
				addendum.Annotations = map[string]string{retiredLayerAnnotationNydusBlob: "true"}
			}
			img, err := mutate.Append(empty.Image, addendum)
			if err != nil {
				t.Fatal(err)
			}
			img = mutate.MediaType(img, types.OCIManifestSchema1)
			if strings.HasPrefix(marker, "manifest-") {
				annotation := retiredLayerAnnotationNydusBootstrap
				if marker == "manifest-blob" {
					annotation = retiredLayerAnnotationNydusBlob
				}
				img = mutate.Annotations(img, map[string]string{annotation: "true"}).(v1.Image)
			}
			if err := remote.Write(ref, img, remote.WithAuth(&authn.Basic{Username: "agent", Password: "secret"})); err != nil {
				t.Fatal(err)
			}
			blobReads.Store(0)
			mgr := newTestManager(t)
			defer mgr.store.close()
			mgr.registry, err = imageregistry.NewClient("")
			if err != nil {
				t.Fatal(err)
			}
			mgr.mountFn = func(string, []string) error {
				t.Error("rejected image reached overlay mount")
				return nil
			}
			auth := ""
			if marker == "inline-auth" {
				config, err := json.Marshal(map[string]any{"auths": map[string]any{host: map[string]string{"auth": base64.StdEncoding.EncodeToString([]byte("agent:secret"))}}})
				if err != nil {
					t.Fatal(err)
				}
				auth = string(config)
			}
			if _, err := mgr.MountImageWithContextAndAuth(t.Context(), ref.Name(), auth); !errors.Is(err, ErrNydusImage) {
				t.Fatalf("MountImage() error = %v, want ErrNydusImage", err)
			}
			if count := blobReads.Load(); count != 0 {
				t.Fatalf("rejected image fetched %d content blobs", count)
			}
			assertNoImageMountState(t, mgr)
		})
	}
}

type brokenManifestImage struct{ v1.Image }

func (brokenManifestImage) Manifest() (*v1.Manifest, error) {
	return nil, io.ErrUnexpectedEOF
}

func TestValidateOCIImageRejectsUnreadableManifest(t *testing.T) {
	if err := validateOCIImage(brokenManifestImage{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("validateOCIImage() error = %v, want manifest error", err)
	}
}

func TestImportRejectsNydusBeforePublishingContentOrRef(t *testing.T) {
	for _, format := range []string{"source-media-type", "source-bootstrap-annotation", "source-blob-annotation", "rafs-v5", "rafs-v6"} {
		t.Run(format, func(t *testing.T) {
			mgr := newTestManager(t)
			defer mgr.store.close()
			const imageRef = "example.local/import-format:test"
			prior, err := mgr.ImportImageArchive(t.Context(), imageRef, writeMountableDockerArchive(t, imageRef))
			if err != nil {
				t.Fatal(err)
			}
			priorBytes, err := os.ReadFile(prior.ArchivePath)
			if err != nil {
				t.Fatal(err)
			}
			priorMount := &ContainerInfo{ImageURL: imageRef, MountPath: t.TempDir()}
			mgr.setContainer(importedCacheKey(prior.ContentDigest), priorMount)

			payload := []byte("ordinary data")
			if strings.HasPrefix(format, "rafs-") {
				payload = rafsBootstrapFixture(format)
			}
			archivePath := formatDockerArchive(t, imageRef, formatLayerTar(t, retiredBootstrapFileNameInLayer, payload))
			if strings.HasPrefix(format, "source-") {
				addArchiveSourceMarker(t, archivePath, format)
			}
			if _, err := mgr.ImportImageArchive(t.Context(), imageRef, archivePath); !errors.Is(err, ErrNydusImage) {
				t.Fatalf("ImportImageArchive() error = %v, want ErrNydusImage", err)
			}
			key, imported, err := mgr.ResolveImportedImageCacheKey(imageRef)
			if err != nil || !imported || key != importedCacheKey(prior.ContentDigest) {
				t.Fatalf("previous ref changed: (%q, %t, %v)", key, imported, err)
			}
			contents, err := mgr.store.listImportContents()
			if err != nil || len(contents) != 1 {
				t.Fatalf("rejected content was published: (%+v, %v)", contents, err)
			}
			currentBytes, err := os.ReadFile(prior.ArchivePath)
			if err != nil || !bytes.Equal(priorBytes, currentBytes) {
				t.Fatalf("previous archive changed: %v", err)
			}
			if got := mgr.getContainer(key); !reflect.DeepEqual(got, priorMount) {
				t.Fatalf("existing mount changed: %+v", got)
			}
			staged, err := os.ReadDir(filepath.Join(mgr.importsDir, "staging"))
			if err != nil || len(staged) != 0 {
				t.Fatalf("rejected import staging remains: (%v, %v)", staged, err)
			}
		})
	}
}

func TestMountRejectsPreviouslyImportedNydus(t *testing.T) {
	for _, format := range []string{"rafs-v6", "source-bootstrap-annotation"} {
		t.Run(format, func(t *testing.T) {
			mgr := newTestManager(t)
			defer mgr.store.close()
			const imageRef = "example.local/old-import:test"
			payload := []byte("data without a RAFS signature")
			if format == "rafs-v6" {
				payload = rafsBootstrapFixture(format)
			}
			archive := formatDockerArchive(t, imageRef, formatLayerTar(t, retiredBootstrapFileNameInLayer, payload))
			if format == "source-bootstrap-annotation" {
				addArchiveSourceMarker(t, archive, format)
			}
			image, err := tarball.ImageFromPath(archive, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateOCIImage(image); err != nil {
				t.Fatalf("fixture should have no manifest markers: %v", err)
			}
			digest, err := image.Digest()
			if err != nil {
				t.Fatal(err)
			}
			path, err := mgr.importBlobPath(digest.String())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			if err := mgr.store.putImport(&importedRefRecord{ImageURL: imageRef, ContentDigest: digest.String()}, &importedContentRecord{ContentDigest: digest.String(), ArchivePath: path}); err != nil {
				t.Fatal(err)
			}
			if _, err := mgr.MountImageWithContext(t.Context(), imageRef); !errors.Is(err, ErrNydusImage) {
				t.Fatalf("MountImage() error = %v, want ErrNydusImage", err)
			}
			assertNoImageMountState(t, mgr)
			if retained, err := mgr.HasImportedImage(imageRef); err != nil || !retained {
				t.Fatalf("existing imported record was removed: (%t, %v)", retained, err)
			}
		})
	}
}

func TestMountRejectsReusedNydusRootfsWithoutRemovingState(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(map[bool]string{false: "persisted", true: "in-memory"}[cached], func(t *testing.T) {
			mgr := newTestManager(t)
			defer mgr.store.close()
			const imageRef = "example.local/previously-mounted:test"
			mountPath := t.TempDir()
			if err := os.Mkdir(filepath.Join(mountPath, "image"), 0755); err != nil {
				t.Fatal(err)
			}
			bootstrap := filepath.Join(mountPath, retiredBootstrapFileNameInLayer)
			if err := os.WriteFile(bootstrap, rafsBootstrapFixture("rafs-v6"), 0644); err != nil {
				t.Fatal(err)
			}
			record := &OciMountState{ImageURL: imageRef, CacheKey: imageRef, MountID: "previous", MountPath: mountPath, ImageConfig: &ImageConfig{}}
			if err := mgr.store.putMount(record); err != nil {
				t.Fatal(err)
			}
			if cached {
				mgr.setContainer(imageRef, &ContainerInfo{ImageURL: imageRef, MountID: "previous", MountPath: mountPath, ImageConfig: &ImageConfig{}})
			}
			mgr.unmountFn = func(string) error {
				t.Error("format rejection must not unmount existing rootfs")
				return nil
			}
			if _, err := mgr.MountImageWithContext(t.Context(), imageRef); !errors.Is(err, ErrNydusImage) {
				t.Fatalf("MountImage() error = %v, want ErrNydusImage", err)
			}
			retained, err := mgr.store.getMount(imageRef)
			if err != nil || !reflect.DeepEqual(retained, record) {
				t.Fatalf("existing mount changed: (%+v, %v)", retained, err)
			}
			if _, err := os.Stat(bootstrap); err != nil {
				t.Fatalf("existing mount data removed: %v", err)
			}
		})
	}
}

func TestEnsureLayerRejectsCachedOrRecoveredNydusWithoutMutatingRecords(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(map[bool]string{false: "recovered", true: "cached"}[persisted], func(t *testing.T) {
			mgr := newTestManager(t)
			defer mgr.store.close()
			digest := v1.Hash{Algorithm: "sha256", Hex: strings.Repeat("a", 64)}
			dir, err := mgr.store.getOrCreateLayerDir(digest.String())
			if err != nil {
				t.Fatal(err)
			}
			layerPath := filepath.Join(mgr.layersDir, dir, "fs")
			if err := os.MkdirAll(filepath.Join(layerPath, "image"), 0755); err != nil {
				t.Fatal(err)
			}
			bootstrapPath := filepath.Join(layerPath, retiredBootstrapFileNameInLayer)
			if err := os.WriteFile(bootstrapPath, rafsBootstrapFixture("rafs-v5"), 0644); err != nil {
				t.Fatal(err)
			}
			var prior *LayerRecord
			if persisted {
				prior = &LayerRecord{Digest: digest.String(), Path: layerPath, RefCount: 3, LastUsedUnix: 42}
				if err := mgr.store.putLayer(prior); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := mgr.ensureLayerExtracted(panicUncompressedLayer{digest: digest}); !errors.Is(err, ErrNydusImage) {
				t.Fatalf("ensureLayerExtracted() error = %v, want ErrNydusImage", err)
			}
			current, err := mgr.store.getLayer(digest.String())
			if err != nil || !reflect.DeepEqual(current, prior) {
				t.Fatalf("existing record mutated: (%+v, %v)", current, err)
			}
			if _, err := os.Stat(bootstrapPath); err != nil {
				t.Fatalf("existing layer removed: %v", err)
			}
		})
	}
}

func TestOrdinaryBootstrapNamedFilesRemainUsable(t *testing.T) {
	erofs := rafsBootstrapFixture("rafs-v6")
	binary.LittleEndian.PutUint32(erofs[1032:1036], 0)
	for _, test := range []struct {
		name, path string
		content    []byte
	}{
		{"ordinary-text", retiredBootstrapFileNameInLayer, []byte("ordinary application data")},
		{"ordinary-erofs", retiredBootstrapFileNameInLayer, erofs},
		{"rafs-asset-at-another-path", "assets/image.boot", rafsBootstrapFixture("rafs-v5")},
	} {
		t.Run(test.name, func(t *testing.T) {
			mgr := newTestManager(t)
			defer mgr.store.close()
			const imageRef = "example.local/ordinary-bootstrap:test"
			archive := formatDockerArchive(t, imageRef, formatLayerTar(t, test.path, test.content))
			if _, err := mgr.ImportImageArchive(t.Context(), imageRef, archive); err != nil {
				t.Fatalf("ImportImageArchive() rejected ordinary data: %v", err)
			}
			if _, err := mgr.MountImageWithContext(t.Context(), imageRef); err != nil {
				t.Fatalf("MountImage() rejected ordinary data: %v", err)
			}
		})
	}
}

func assertNoImageMountState(t *testing.T, mgr *Manager) {
	t.Helper()
	mounts, err := mgr.store.listMounts()
	if err != nil || len(mounts) != 0 {
		t.Fatalf("unexpected mount records: (%+v, %v)", mounts, err)
	}
	txns, err := mgr.store.listMountTxns()
	if err != nil || len(txns) != 0 {
		t.Fatalf("unexpected mount transactions: (%+v, %v)", txns, err)
	}
	layers, err := mgr.store.listLayers()
	if err != nil {
		t.Fatal(err)
	}
	for _, layer := range layers {
		if layer.RefCount != 0 {
			t.Fatalf("unexpected reserved layer: %+v", layer)
		}
	}
}

func rafsBootstrapFixture(format string) []byte {
	// Format fixtures contain real superblock signatures, not complete mountable
	// filesystems. These guards recognize representation before runtime mounting.
	if format == "rafs-v5" {
		data := make([]byte, 8200)
		binary.LittleEndian.PutUint32(data[0:4], 0x52414653)
		binary.LittleEndian.PutUint32(data[4:8], 0x500)
		binary.LittleEndian.PutUint32(data[8:12], 8192)
		binary.LittleEndian.PutUint32(data[12:16], 4096)
		binary.LittleEndian.PutUint64(data[24:32], 1)
		binary.LittleEndian.PutUint64(data[32:40], 8192)
		binary.LittleEndian.PutUint64(data[48:56], 8200)
		binary.LittleEndian.PutUint32(data[56:60], 1)
		return data
	}
	data := make([]byte, 4096)
	binary.LittleEndian.PutUint32(data[1024:1028], 0xe0f5e1e2)
	binary.LittleEndian.PutUint32(data[1032:1036], 0x40000000)
	data[1036] = 12
	binary.LittleEndian.PutUint64(data[1040:1048], 1)
	binary.LittleEndian.PutUint32(data[1104:1108], 0xc)
	binary.LittleEndian.PutUint16(data[1112:1114], 11)
	return data
}

func formatLayerTar(t *testing.T, path string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	if err := writer.WriteHeader(&tar.Header{Name: path, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func formatDockerArchive(t *testing.T, ref string, layer []byte) string {
	t.Helper()
	img, err := mutate.AppendLayers(empty.Image, static.NewLayer(layer, types.OCIUncompressedLayer))
	if err != nil {
		t.Fatal(err)
	}
	parsedRef, err := name.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := tarball.WriteToFile(path, parsedRef, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func addArchiveSourceMarker(t *testing.T, path, format string) {
	t.Helper()
	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(bytes.NewReader(data))
	var changed bytes.Buffer
	writer := tar.NewWriter(&changed)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "manifest.json" {
			var manifest tarball.Manifest
			if err := json.Unmarshal(body, &manifest); err != nil {
				t.Fatal(err)
			}
			source := v1.Descriptor{MediaType: types.OCIUncompressedLayer, Digest: config.RootFS.DiffIDs[0], Size: 1}
			switch format {
			case "source-media-type":
				source.MediaType = types.MediaType(retiredMediaTypeNydusBlob)
			case "source-bootstrap-annotation":
				source.Annotations = map[string]string{retiredLayerAnnotationNydusBootstrap: "true"}
			case "source-blob-annotation":
				source.Annotations = map[string]string{retiredLayerAnnotationNydusBlob: "true"}
			}
			manifest[0].LayerSources = map[v1.Hash]v1.Descriptor{config.RootFS.DiffIDs[0]: source}
			body, err = json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			header.Size = int64(len(body))
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, changed.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}
