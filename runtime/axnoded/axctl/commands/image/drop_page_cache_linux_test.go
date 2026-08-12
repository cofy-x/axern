//go:build linux

package image

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDropMountedFilePageCacheRejectsSymlinkAndEvictsRegularFile(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "qualification")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	pageSize := os.Getpagesize()
	payload := make([]byte, 2*pageSize)
	file := filepath.Join(directory, "payload.bin")
	if err := os.WriteFile(file, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(directory, "payload-link.bin")); err != nil {
		t.Fatal(err)
	}

	if err := dropMountedFilePageCache(root, "/qualification/payload.bin", 0, int64(len(payload))); err != nil {
		t.Fatalf("dropMountedFilePageCache() error = %v", err)
	}
	if err := dropMountedFilePageCache(root, "/qualification/payload-link.bin", 0, int64(len(payload))); err == nil {
		t.Fatal("dropMountedFilePageCache() followed symlink, want error")
	}
}

func TestDropMountedFilePageCacheValidatesDirectCallBoundary(t *testing.T) {
	pageSize := int64(os.Getpagesize())
	for name, call := range map[string]func() error{
		"relative mount":   func() error { return dropMountedFilePageCache("relative", "/file", 0, pageSize) },
		"relative file":    func() error { return dropMountedFilePageCache(t.TempDir(), "file", 0, pageSize) },
		"negative offset":  func() error { return dropMountedFilePageCache(t.TempDir(), "/file", -1, pageSize) },
		"unaligned length": func() error { return dropMountedFilePageCache(t.TempDir(), "/file", 0, pageSize+1) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("dropMountedFilePageCache() error = nil, want validation error")
			}
		})
	}
}
