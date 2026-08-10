//go:build linux

package service

import (
	"os"
	"path/filepath"
	"testing"
)

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
