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
	if got, want := fileInfo.Mode()&(os.ModePerm|os.ModeSetuid), os.FileMode(0755)|os.ModeSetuid; got != want {
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

func TestEnsureLayerExtractedRejectsCanonicalWithoutFacts(t *testing.T) {
	mgr := newTestManager(t)
	layer := sleepLayer{digest: v1.Hash{Algorithm: "sha256", Hex: "canonical-without-facts"}}
	dir, err := mgr.store.getOrCreateLayerDir(layer.digest.String())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(mgr.layersDir, dir, "fs")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "retained"), []byte("retained"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.ensureLayerExtracted(layer); err == nil {
		t.Fatal("canonical content without facts was admitted")
	}
	if data, err := os.ReadFile(filepath.Join(target, "retained")); err != nil || string(data) != "retained" {
		t.Fatalf("existing content changed: %q %v", data, err)
	}
	if record, err := mgr.store.getLayer(layer.digest.String()); err != nil || record != nil {
		t.Fatalf("unknown facts were stamped: %+v %v", record, err)
	}
}

func TestEnsureLayerExtractedRejectsNonContentPath(t *testing.T) {
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

	if err := mgr.store.putLayer(&LayerRecord{
		Digest:        hash.String(),
		Path:          legacyPath,
		RefCount:      0,
		RefZeroAtUnix: 0,
		LastUsedUnix:  0,
	}); err != nil {
		t.Fatalf("put legacy layer: %v", err)
	}

	if _, err := mgr.ensureLayerExtracted(panicUncompressedLayer{digest: hash}); err == nil {
		t.Fatal("non-content-addressed layer metadata was accepted")
	}
}
