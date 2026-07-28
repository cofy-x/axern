package oci

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestExtractLayerTar_HandlesOCIWhiteouts(t *testing.T) {
	dst := t.TempDir()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	writeDir := func(name string) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0755}); err != nil {
			t.Fatalf("write dir %s: %v", name, err)
		}
	}
	writeReg := func(name, content string) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(content))}); err != nil {
			t.Fatalf("write file header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write file body %s: %v", name, err)
		}
	}

	writeDir("etc")
	writeReg("etc/config", "v1")
	writeReg("etc/.wh.config", "")
	writeDir("var")
	writeReg("var/.wh..wh..opq", "")

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if _, err := extractLayerTar(bytes.NewReader(buf.Bytes()), dst); err != nil {
		t.Fatalf("extractLayerTar() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "etc", ".wh.config")); !os.IsNotExist(err) {
		t.Fatalf(".wh.config should not exist as plain whiteout marker, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "etc", "config")); err != nil {
		t.Fatalf("whiteout target should exist after conversion, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "var", ".wh..wh..opq")); !os.IsNotExist(err) {
		t.Fatalf(".wh..wh..opq should not exist as plain file, err=%v", err)
	}
}

func TestExtractLayerTar_PreservesModeRegardlessOfUmask(t *testing.T) {
	dst := t.TempDir()

	oldUmask := syscall.Umask(0077)
	defer syscall.Umask(oldUmask)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin",
		Typeflag: tar.TypeDir,
		Mode:     0755,
	}); err != nil {
		t.Fatalf("write dir header: %v", err)
	}

	content := []byte("echo test\n")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin/run.sh",
		Typeflag: tar.TypeReg,
		Mode:     0777,
		Size:     int64(len(content)),
	}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if _, err := extractLayerTar(bytes.NewReader(buf.Bytes()), dst); err != nil {
		t.Fatalf("extractLayerTar() error: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Join(dst, "bin"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0755); got != want {
		t.Fatalf("dir mode mismatch: got %04o want %04o", got, want)
	}

	fileInfo, err := os.Stat(filepath.Join(dst, "bin", "run.sh"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got, want := fileInfo.Mode().Perm(), os.FileMode(0777); got != want {
		t.Fatalf("file mode mismatch: got %04o want %04o", got, want)
	}
}

func TestFileModeFromTar_PreservesSpecialPermissionBits(t *testing.T) {
	got := fileModeFromTar(06755)
	want := os.FileMode(0755) | os.ModeSetuid | os.ModeSetgid
	if got != want {
		t.Fatalf("mode mismatch: got %v want %v", got, want)
	}

	got = fileModeFromTar(01777)
	want = os.FileMode(0777) | os.ModeSticky
	if got != want {
		t.Fatalf("sticky mode mismatch: got %v want %v", got, want)
	}
}

func TestExtractLayerTar_PreservesSetuidMode(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("preserving setuid after ownership restore requires root")
	}
	dst := t.TempDir()

	content := []byte("#!/bin/sh\n")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "usr/bin/sudo",
		Typeflag: tar.TypeReg,
		Mode:     04755,
		Uid:      0,
		Gid:      0,
		Size:     int64(len(content)),
	}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if _, err := extractLayerTar(bytes.NewReader(buf.Bytes()), dst); err != nil {
		t.Fatalf("extractLayerTar() error: %v", err)
	}

	fileInfo, err := os.Stat(filepath.Join(dst, "usr", "bin", "sudo"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got, want := fileInfo.Mode()&os.ModePerm|fileInfo.Mode()&os.ModeSetuid, os.FileMode(04755); got != want {
		t.Fatalf("file mode mismatch: got %04o want %04o", got, want)
	}
}

func TestExtractLayerTar_HardlinkDoesNotClobberTargetMode(t *testing.T) {
	dst := t.TempDir()

	content := []byte("binary\n")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin/tool",
		Typeflag: tar.TypeReg,
		Mode:     0755,
		Size:     int64(len(content)),
	}); err != nil {
		t.Fatalf("write file header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write file content: %v", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin/tool-link",
		Typeflag: tar.TypeLink,
		Linkname: "bin/tool",
		Mode:     0644,
	}); err != nil {
		t.Fatalf("write link header: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if _, err := extractLayerTar(bytes.NewReader(buf.Bytes()), dst); err != nil {
		t.Fatalf("extractLayerTar() error: %v", err)
	}

	fileInfo, err := os.Stat(filepath.Join(dst, "bin", "tool"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	linkInfo, err := os.Stat(filepath.Join(dst, "bin", "tool-link"))
	if err != nil {
		t.Fatalf("stat hardlink: %v", err)
	}
	if !os.SameFile(fileInfo, linkInfo) {
		t.Fatal("tool-link is not a hardlink to tool")
	}
	if got, want := fileInfo.Mode().Perm(), os.FileMode(0755); got != want {
		t.Fatalf("file mode mismatch: got %04o want %04o", got, want)
	}
}

func TestEnsureLayerExtracted_RecoversMetadataFromExistingPath(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	hash, err := v1.NewHash("sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new hash: %v", err)
	}

	layerDir, err := mgr.store.getOrCreateLayerDir(hash.String())
	if err != nil {
		t.Fatalf("getOrCreateLayerDir: %v", err)
	}
	layerPath := filepath.Join(mgr.layersDir, layerDir, "fs")
	if err := os.MkdirAll(filepath.Join(layerPath, "etc"), 0755); err != nil {
		t.Fatalf("mkdir layer path: %v", err)
	}
	content := []byte("hello")
	if err := os.WriteFile(filepath.Join(layerPath, "etc", "config"), content, 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec, err := mgr.ensureLayerExtracted(panicUncompressedLayer{digest: hash})
	if err != nil {
		t.Fatalf("ensureLayerExtracted() error: %v", err)
	}
	if rec == nil {
		t.Fatalf("expected layer record")
	}
	if rec.Path != layerPath {
		t.Fatalf("unexpected recovered path: got %s want %s", rec.Path, layerPath)
	}
	if rec.SizeBytes <= 0 {
		t.Fatalf("expected recovered layer size > 0, got %d", rec.SizeBytes)
	}

	stored, err := mgr.store.getLayer(hash.String())
	if err != nil {
		t.Fatalf("get recovered layer: %v", err)
	}
	if stored == nil {
		t.Fatalf("expected recovered layer metadata in store")
	}
	if stored.RefCount != 1 {
		t.Fatalf("expected recovered layer refcount = 1, got %d", stored.RefCount)
	}
	if stored.RefZeroAtUnix != 0 {
		t.Fatalf("expected recovered layer ref-zero timestamp cleared, got %d", stored.RefZeroAtUnix)
	}
}

func TestEnsureLayerExtracted_KeepLegacyLayerPathNoMigration(t *testing.T) {
	mgr := newTestManager(t)
	defer mgr.store.close()

	hash, err := v1.NewHash("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err != nil {
		t.Fatalf("new hash: %v", err)
	}

	legacyPath := filepath.Join(mgr.layersDir, "legacy-path", "fs")
	if err := os.MkdirAll(filepath.Join(legacyPath, "etc"), 0755); err != nil {
		t.Fatalf("mkdir legacy path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyPath, "etc", "config"), []byte("legacy"), 0644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	layerDir, err := mgr.store.getOrCreateLayerDir(hash.String())
	if err != nil {
		t.Fatalf("getOrCreateLayerDir: %v", err)
	}
	mappedPath := filepath.Join(mgr.layersDir, layerDir, "fs")

	if err := mgr.store.putLayer(&LayerRecord{
		Digest:        hash.String(),
		Path:          legacyPath,
		RefCount:      0,
		RefZeroAtUnix: 0,
		LastUsedUnix:  0,
	}); err != nil {
		t.Fatalf("put legacy layer: %v", err)
	}

	rec, err := mgr.ensureLayerExtracted(panicUncompressedLayer{digest: hash})
	if err != nil {
		t.Fatalf("ensureLayerExtracted() error: %v", err)
	}
	if rec.Path != legacyPath {
		t.Fatalf("legacy layer path should be kept, got %s want %s", rec.Path, legacyPath)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy path should still exist, err=%v", err)
	}
	if _, err := os.Stat(mappedPath); !os.IsNotExist(err) {
		t.Fatalf("mapped path should not be created by online migration, err=%v", err)
	}
	if rec.RefCount != 1 {
		t.Fatalf("expected reserved layer refcount = 1, got %d", rec.RefCount)
	}
	if rec.RefZeroAtUnix != 0 {
		t.Fatalf("expected ref-zero timestamp to be cleared while reserved, got %d", rec.RefZeroAtUnix)
	}
}
