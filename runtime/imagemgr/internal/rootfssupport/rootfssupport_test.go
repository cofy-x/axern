package rootfssupport

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureCreatesExpectedDirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "support", "fs")
	if err := Ensure(root); err != nil {
		t.Fatalf("Ensure() error: %v", err)
	}

	for _, entry := range Dirs() {
		path := filepath.Join(root, entry.Path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s should be a directory", path)
		}
		if got, want := info.Mode().Perm(), entry.Mode.Perm(); got != want {
			t.Fatalf("perm for %s = %04o, want %04o", path, got, want)
		}
		if entry.Mode&os.ModeSticky != 0 && info.Mode()&os.ModeSticky == 0 {
			t.Fatalf("sticky bit missing for %s", path)
		}
	}
}

func TestDirsReturnsCopy(t *testing.T) {
	got := Dirs()
	got[0].Path = "changed"

	if Dirs()[0].Path == "changed" {
		t.Fatal("Dirs() returned mutable package state")
	}
}
