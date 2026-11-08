//go:build linux

package service

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestRuntimeDigestCacheWaitsForCtimeResolutionWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(path, []byte("runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	changed := time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	if runtimeFileMetadataCacheable(info, changed.Add(999*time.Millisecond)) {
		t.Fatal("recent ctime must not seed a cached digest")
	}
	if !runtimeFileMetadataCacheable(info, changed.Add(time.Second)) {
		t.Fatal("stable metadata must retain the installed-runtime cache")
	}
	if runtimeFileMetadataCacheable(info, changed.Add(-time.Second)) {
		t.Fatal("a future timestamp must not seed a cached digest")
	}
}

func TestRuntimeDigestCacheDetectsRewriteWithPreservedSizeAndMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime")
	if err := os.WriteFile(path, []byte("one"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	cache := newRuntimeFileDigestCache()
	before, err := cache.Digest(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, err := cache.Digest(path)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("runtime digest cache reused stale digest %q after in-place rewrite", before)
	}
}
