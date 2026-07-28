package ossloop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/rootfssupport"
)

func TestManagerMountReuseAndRefcount(t *testing.T) {
	var (
		mountCalls   int
		unmountCalls int
		mountedPaths = map[string]bool{}
	)
	root := t.TempDir()

	mgr, err := NewManager(&Config{
		Root: root,
		MountFunc: func(_, lowerPath, targetPath, supportPath string) error {
			if lowerPath != filepath.Join(root, "lowers", "abc") {
				t.Fatalf("lowerPath = %q, want lower path for mount id", lowerPath)
			}
			if supportPath != filepath.Join(root, "support", "fs") {
				t.Fatalf("supportPath = %q, want runtime support path", supportPath)
			}
			mountCalls++
			mountedPaths[targetPath] = true
			return nil
		},
		UnmountFn: func(lowerPath, targetPath string) error {
			if lowerPath != filepath.Join(root, "lowers", "abc") {
				t.Fatalf("unmount lowerPath = %q, want lower path for mount id", lowerPath)
			}
			unmountCalls++
			delete(mountedPaths, targetPath)
			return nil
		},
		MountedFn: func(targetPath string) (bool, error) {
			return mountedPaths[targetPath], nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	path1, err := mgr.Mount("abc", "/fuse/rootfs.ext4")
	if err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	path2, err := mgr.Mount("abc", "/fuse/rootfs.ext4")
	if err != nil {
		t.Fatalf("Mount() second error = %v", err)
	}
	if path1 != path2 {
		t.Fatalf("Mount() returned different paths: %q vs %q", path1, path2)
	}
	if mountCalls != 1 {
		t.Fatalf("mountCalls = %d, want 1", mountCalls)
	}

	result, err := mgr.Unmount("abc")
	if err != nil {
		t.Fatalf("Unmount() first error = %v", err)
	}
	if result.Released {
		t.Fatal("first Unmount() released mount, want refcount-only decrement")
	}
	if unmountCalls != 0 {
		t.Fatalf("unmountCalls = %d after first unmount, want 0", unmountCalls)
	}

	result, err = mgr.Unmount("abc")
	if err != nil {
		t.Fatalf("Unmount() second error = %v", err)
	}
	if !result.Released {
		t.Fatal("second Unmount() did not release mount")
	}
	if unmountCalls != 1 {
		t.Fatalf("unmountCalls = %d, want 1", unmountCalls)
	}
}

func TestManagerEnsureMountedKeepsSingleResourceReference(t *testing.T) {
	mountCalls := 0
	unmountCalls := 0
	mountedPaths := map[string]bool{}
	mgr, err := NewManager(&Config{
		Root: t.TempDir(),
		MountFunc: func(_, _, targetPath, _ string) error {
			mountCalls++
			mountedPaths[targetPath] = true
			return nil
		},
		UnmountFn: func(_, targetPath string) error {
			unmountCalls++
			delete(mountedPaths, targetPath)
			return nil
		},
		MountedFn: func(targetPath string) (bool, error) { return mountedPaths[targetPath], nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnsureMounted("resource", "/fuse/rootfs.ext4"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnsureMounted("resource", "/fuse/rootfs.ext4"); err != nil {
		t.Fatal(err)
	}
	result, err := mgr.Unmount("resource")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Released || mountCalls != 1 || unmountCalls != 1 {
		t.Fatalf("released=%v mount_calls=%d unmount_calls=%d", result.Released, mountCalls, unmountCalls)
	}
	result, err = mgr.ReleaseResource("resource")
	if err != nil || !result.Released || unmountCalls != 1 {
		t.Fatalf("idempotent release = %+v, %v; unmount_calls=%d", result, err, unmountCalls)
	}
}

func TestManagerRejectsPathLikeMountID(t *testing.T) {
	mgr, err := NewManager(&Config{
		Root: t.TempDir(),
		MountFunc: func(_, _, _, _ string) error {
			return nil
		},
		UnmountFn: func(_, _ string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	for _, id := range []string{"nested/id", "../id", ".", " padded"} {
		if _, err := mgr.Mount(id, "/fuse/rootfs.ext4"); err == nil {
			t.Fatalf("Mount(%q) error = nil, want invalid id error", id)
		}
		if _, err := mgr.Unmount(id); err == nil {
			t.Fatalf("Unmount(%q) error = nil, want invalid id error", id)
		}
	}
}

func TestNewManagerPreparesRuntimeSupportDirs(t *testing.T) {
	root := t.TempDir()
	_, err := NewManager(&Config{
		Root: root,
		MountFunc: func(_, _, _, _ string) error {
			return nil
		},
		UnmountFn: func(_, _ string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	for _, entry := range rootfssupport.Dirs() {
		path := filepath.Join(root, "support", "fs", entry.Path)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat support dir %s: %v", entry.Path, err)
		}
		if !info.IsDir() {
			t.Fatalf("support path %s should be a directory", entry.Path)
		}
	}
}

func TestManagerRecoversPersistedMount(t *testing.T) {
	var (
		mountCalls   int
		unmountCalls int
		mountedPaths = map[string]bool{}
	)
	root := t.TempDir()

	first, err := NewManager(&Config{
		Root: root,
		MountFunc: func(_, _, targetPath, _ string) error {
			mountCalls++
			mountedPaths[targetPath] = true
			return nil
		},
		UnmountFn: func(_, targetPath string) error {
			unmountCalls++
			delete(mountedPaths, targetPath)
			return nil
		},
		MountedFn: func(targetPath string) (bool, error) {
			return mountedPaths[targetPath], nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	mountPath, err := first.Mount("persisted", "/fuse/persisted.ext4")
	if err != nil {
		t.Fatalf("Mount() error = %v", err)
	}
	if want := filepath.Join(root, "mounts", "persisted"); mountPath != want {
		t.Fatalf("Mount() path = %q, want %q", mountPath, want)
	}

	second, err := NewManager(&Config{
		Root: root,
		MountFunc: func(_, _, targetPath, _ string) error {
			mountCalls++
			mountedPaths[targetPath] = true
			return nil
		},
		UnmountFn: func(_, targetPath string) error {
			unmountCalls++
			delete(mountedPaths, targetPath)
			return nil
		},
		MountedFn: func(targetPath string) (bool, error) {
			return mountedPaths[targetPath], nil
		},
	})
	if err != nil {
		t.Fatalf("NewManager() second error = %v", err)
	}

	reusedPath, err := second.Mount("persisted", "/fuse/persisted.ext4")
	if err != nil {
		t.Fatalf("Mount() after recovery error = %v", err)
	}
	if reusedPath != mountPath {
		t.Fatalf("reused mount path = %q, want %q", reusedPath, mountPath)
	}
	if mountCalls != 1 {
		t.Fatalf("mountCalls = %d, want 1", mountCalls)
	}

	result, err := second.Unmount("persisted")
	if err != nil {
		t.Fatalf("Unmount() after recovery error = %v", err)
	}
	if !result.Released {
		t.Fatal("Unmount() after recovery did not release mount")
	}
	if unmountCalls != 1 {
		t.Fatalf("unmountCalls = %d, want 1", unmountCalls)
	}
}
