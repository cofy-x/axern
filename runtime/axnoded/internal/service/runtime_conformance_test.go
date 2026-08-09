package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeRuntimeConformanceRootfs(t *testing.T) {
	filestore := t.TempDir()
	fixture := t.TempDir()
	fixtureBin := filepath.Join(fixture, "bin")
	if err := os.Mkdir(fixtureBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureBin, "busybox"), []byte("fixture"), 0o755); err != nil {
		t.Fatal(err)
	}

	rootfs, err := materializeRuntimeConformanceRootfs(filestore, fixture)
	if err != nil {
		t.Fatalf("materializeRuntimeConformanceRootfs() error = %v", err)
	}
	if err := validateRuntimeConformanceRootfs(rootfs); err != nil {
		t.Fatalf("validateRuntimeConformanceRootfs() error = %v", err)
	}
	second, err := materializeRuntimeConformanceRootfs(filestore, fixture)
	if err != nil {
		t.Fatalf("second materializeRuntimeConformanceRootfs() error = %v", err)
	}
	if second != rootfs {
		t.Fatalf("second rootfs = %q, want %q", second, rootfs)
	}
}

func TestMaterializeRuntimeConformanceRootfsRejectsCorruptPublishedFixture(t *testing.T) {
	filestore := t.TempDir()
	rootfs := filepath.Join(filestore, "system", "runtime-conformance", "rootfs")
	if err := os.MkdirAll(rootfs, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := materializeRuntimeConformanceRootfs(filestore, t.TempDir()); err == nil {
		t.Fatal("materializeRuntimeConformanceRootfs() error = nil, want corrupt fixture rejection")
	}
}
