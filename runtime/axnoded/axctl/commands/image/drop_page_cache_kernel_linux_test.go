//go:build linux && kerneltruth

package image

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDropMountedFilePageCacheEvictsRegularFile(t *testing.T) {
	root := t.TempDir()
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	switch fs.Type {
	case unix.EXT4_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC:
	default:
		t.Fatalf("kernel truth requires a disk-backed ext4, XFS or Btrfs TMPDIR; got filesystem %#x", fs.Type)
	}
	file, err := os.Create(filepath.Join(root, "payload.bin"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	payload := make([]byte, 2*os.Getpagesize())
	if _, err := file.Write(payload); err != nil {
		t.Fatal(err)
	}
	// DONTNEED does not promise to evict dirty pages. Prepare a clean,
	// resident fixture rather than depending on background writeback timing.
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.ReadAt(payload, 0); err != nil {
		t.Fatal(err)
	}
	resident, err := residentPageCount(int(file.Fd()), 0, int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if resident != 2 {
		t.Fatalf("fixture has %d resident pages, want 2", resident)
	}
	if err := dropMountedFilePageCache(root, "/payload.bin", 0, int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	resident, err = residentPageCount(int(file.Fd()), 0, int64(len(payload)))
	if err != nil {
		t.Fatal(err)
	}
	if resident != 0 {
		t.Fatalf("after eviction: %d resident pages", resident)
	}
}
