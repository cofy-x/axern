package oci

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	bolt "go.etcd.io/bbolt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestImportImageArchiveMountsFromLocalArchive(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	imageRef := "example.local/imported:dev"
	archivePath := writeMountableDockerArchive(t, imageRef)
	result, err := mgr.ImportImageArchive(context.Background(), imageRef, archivePath)
	if err != nil {
		t.Fatalf("ImportImageArchive() error: %v", err)
	}
	if result.ImageURL != imageRef {
		t.Fatalf("ImageURL = %q, want %q", result.ImageURL, imageRef)
	}
	if result.ArchiveDigest == "" {
		t.Fatal("ArchiveDigest is empty")
	}

	mountResult, err := mgr.MountImageWithContext(context.Background(), imageRef)
	if err != nil {
		t.Fatalf("MountImageWithContext() error: %v", err)
	}
	if mountResult == nil || mountResult.MountPath == "" {
		t.Fatal("MountImageWithContext() mountPath empty")
	}

	imports, err := mgr.ListImportedImages()
	if err != nil {
		t.Fatalf("ListImportedImages() error: %v", err)
	}
	if len(imports) != 1 || imports[0].ImageURL != imageRef {
		t.Fatalf("imports = %+v, want one record for %s", imports, imageRef)
	}
	if imports[0].ArchiveDigest != result.ArchiveDigest {
		t.Fatalf("import digest = %q, want %q", imports[0].ArchiveDigest, result.ArchiveDigest)
	}
}

func TestImportImageArchiveRejectsInvalidArchive(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	archivePath := filepath.Join(t.TempDir(), "bad.tar")
	if err := os.WriteFile(archivePath, []byte("not a docker archive"), 0644); err != nil {
		t.Fatalf("write invalid archive: %v", err)
	}
	if _, err := mgr.ImportImageArchive(context.Background(), "example.local/bad:dev", archivePath); err == nil {
		t.Fatal("ImportImageArchive() error = nil, want non-nil")
	}
}

func TestImportImageArchivePreservesExistingInMemoryMount(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	imageRef := "example.local/imported-refresh:dev"
	mountPath := filepath.Join(t.TempDir(), "mount")
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		t.Fatalf("mkdir mount path: %v", err)
	}
	mgr.setContainer(imageRef, &ContainerInfo{
		ImageURL:  imageRef,
		MountPath: mountPath,
	})

	mgr.unmountFn = func(target string) error {
		t.Fatalf("ImportImageArchive() unexpectedly unmounted %q", target)
		return nil
	}

	if _, err := mgr.ImportImageArchive(context.Background(), imageRef, writeMountableDockerArchive(t, imageRef)); err != nil {
		t.Fatalf("ImportImageArchive() error: %v", err)
	}
	if info := mgr.getContainer(imageRef); info == nil || info.MountPath != mountPath {
		t.Fatalf("container after import = %+v, want existing mount path %q", info, mountPath)
	}
}

func TestHasImportedImage(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	imageRef := "example.local/has-import:dev"
	ok, err := mgr.HasImportedImage(imageRef)
	if err != nil {
		t.Fatalf("HasImportedImage() error before import: %v", err)
	}
	if ok {
		t.Fatal("HasImportedImage() = true before import, want false")
	}

	if _, err := mgr.ImportImageArchive(context.Background(), imageRef, writeMountableDockerArchive(t, imageRef)); err != nil {
		t.Fatalf("ImportImageArchive() error: %v", err)
	}
	ok, err = mgr.HasImportedImage(imageRef)
	if err != nil {
		t.Fatalf("HasImportedImage() error after import: %v", err)
	}
	if !ok {
		t.Fatal("HasImportedImage() = false after import, want true")
	}

	cacheKey, imported, err := mgr.ResolveImportedImageCacheKey(imageRef)
	if err != nil {
		t.Fatalf("ResolveImportedImageCacheKey() error: %v", err)
	}
	if !imported {
		t.Fatal("ResolveImportedImageCacheKey() imported = false, want true")
	}
	if cacheKey == imageRef {
		t.Fatalf("ResolveImportedImageCacheKey() = %q, want content-addressed key", cacheKey)
	}
}

func TestImportImageMovesRefWithoutInvalidatingMountedGeneration(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	imageRef := "example.local/mutable:dev"
	first, err := mgr.ImportImageArchive(t.Context(), imageRef, writeMountableDockerArchiveWithContent(t, imageRef, "first"))
	if err != nil {
		t.Fatal(err)
	}
	firstKey := importedCacheKey(first.GenerationDigest)
	mgr.setContainer(firstKey, &ContainerInfo{ImageURL: imageRef, MountPath: t.TempDir()})

	second, err := mgr.ImportImageArchive(t.Context(), imageRef, writeMountableDockerArchiveWithContent(t, imageRef, "second"))
	if err != nil {
		t.Fatal(err)
	}
	if first.GenerationDigest == second.GenerationDigest {
		t.Fatal("distinct manifests produced the same generation")
	}
	current, imported, err := mgr.ResolveImportedImageCacheKey(imageRef)
	if err != nil || !imported || current != importedCacheKey(second.GenerationDigest) {
		t.Fatalf("current generation = (%q, %t, %v)", current, imported, err)
	}
	if retained, err := mgr.HasImportedGeneration(firstKey); err != nil || !retained {
		t.Fatalf("old mounted generation retained = (%t, %v)", retained, err)
	}
	if _, err := os.Stat(first.ArchivePath); err != nil {
		t.Fatalf("old generation archive removed while mounted: %v", err)
	}

	mgr.deleteContainer(firstKey)
	if err := mgr.pruneImportedGenerations(); err != nil {
		t.Fatal(err)
	}
	if retained, err := mgr.HasImportedGeneration(firstKey); err != nil || retained {
		t.Fatalf("unreferenced generation retained = (%t, %v)", retained, err)
	}
}

func TestImportImageCanonicalizesDockerHubRef(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()
	result, err := mgr.ImportImageArchive(t.Context(), "demo:dev", writeMountableDockerArchive(t, "demo:dev"))
	if err != nil {
		t.Fatal(err)
	}
	if result.ImageURL != "index.docker.io/library/demo:dev" {
		t.Fatalf("canonical ref = %q", result.ImageURL)
	}
	canonical, cacheKey, imported, err := mgr.ResolveImageCacheKey("docker.io/library/demo:dev")
	if err != nil || !imported || canonical != result.ImageURL || cacheKey != importedCacheKey(result.GenerationDigest) {
		t.Fatalf("ResolveImageCacheKey() = (%q, %q, %t, %v)", canonical, cacheKey, imported, err)
	}
	digestRef := "index.docker.io/library/demo@" + result.GenerationDigest
	_, cacheKey, imported, err = mgr.ResolveImageCacheKey(digestRef)
	if err != nil || !imported || cacheKey != importedCacheKey(result.GenerationDigest) {
		t.Fatalf("resolve digest generation = (%q, %t, %v)", cacheKey, imported, err)
	}
}

func TestResolveImportedImageCacheKeyRejectsMissingGeneration(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	imageRef := "example.local/missing-digest:dev"
	if err := mgr.store.db.Update(func(tx *bolt.Tx) error {
		data, err := json.Marshal(importedRefRecord{ImageURL: imageRef, GenerationDigest: "sha256:" + strings.Repeat("0", 64)})
		if err != nil {
			return err
		}
		return tx.Bucket(importRefsBucket).Put([]byte(imageRef), data)
	}); err != nil {
		t.Fatalf("put broken ref: %v", err)
	}

	if _, _, err := mgr.ResolveImportedImageCacheKey(imageRef); err == nil {
		t.Fatal("ResolveImportedImageCacheKey() error = nil, want missing digest error")
	}
}

func TestImmutableImageRefDropsMutableTag(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err := ImmutableImageRef("index.docker.io/library/demo:dev", digest)
	if err != nil {
		t.Fatalf("ImmutableImageRef() error = %v", err)
	}
	want := "index.docker.io/library/demo@" + digest
	if got != want {
		t.Fatalf("ImmutableImageRef() = %q, want %q", got, want)
	}
	if _, err := ImmutableImageRef("demo:dev", "sha256:invalid"); err == nil {
		t.Fatal("ImmutableImageRef() accepted an invalid digest")
	}
}

func writeMountableDockerArchive(t *testing.T, imageRef string) string {
	return writeMountableDockerArchiveWithContent(t, imageRef, "hello from imported image")
}

func writeMountableDockerArchiveWithContent(t *testing.T, imageRef, value string) string {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	content := []byte(value)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "app/hello.txt",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
	}); err != nil {
		t.Fatalf("WriteHeader() error: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	layer := static.NewLayer(buf.Bytes(), types.OCIUncompressedLayer)
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatalf("AppendLayers() error: %v", err)
	}
	ref, err := name.ParseReference(imageRef, name.WeakValidation)
	if err != nil {
		t.Fatalf("ParseReference() error: %v", err)
	}
	path := filepath.Join(t.TempDir(), "image.tar")
	if err := tarball.WriteToFile(path, ref, img); err != nil {
		t.Fatalf("WriteToFile() error: %v", err)
	}
	return path
}
