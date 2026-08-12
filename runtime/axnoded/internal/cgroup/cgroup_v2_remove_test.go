//go:build linux

package cgroup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCgroupV2RemoveDeletesOnlyKnownHierarchy(t *testing.T) {
	mountpoint := t.TempDir()
	driver := &cgroupV2Driver{mountpoint: mountpoint}
	parent := filepath.Join(mountpoint, "sandbox", "alloc-1")
	if err := os.MkdirAll(filepath.Join(parent, CgroupWorkloadLeafName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := driver.Remove("/sandbox/alloc-1"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("allocation parent still exists or stat failed: %v", err)
	}
}

func TestCgroupV2RemovePreservesUnexpectedSubtree(t *testing.T) {
	mountpoint := t.TempDir()
	driver := &cgroupV2Driver{mountpoint: mountpoint}
	parent := filepath.Join(mountpoint, "sandbox", "alloc-1")
	unexpected := filepath.Join(parent, "unexpected")
	if err := os.MkdirAll(filepath.Join(parent, CgroupWorkloadLeafName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(unexpected, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := driver.Remove("/sandbox/alloc-1"); err == nil {
		t.Fatal("Remove() error = nil, want unexpected subtree failure")
	}
	if info, err := os.Stat(unexpected); err != nil || !info.IsDir() {
		t.Fatalf("unexpected subtree was removed: info=%v err=%v", info, err)
	}
}

func TestCgroupV2RemoveRejectsReservedGroups(t *testing.T) {
	driver := &cgroupV2Driver{mountpoint: t.TempDir(), delegationGroup: "/system.slice/axnoded.service"}
	for _, group := range []string{"/", "/internal", "/system.slice/axnoded.service", "/system.slice/axnoded.service/internal", "/other/sandbox/a", "/system.slice/axnoded.service/sandbox/a/workload"} {
		if err := driver.Remove(group); err == nil {
			t.Fatalf("Remove(%q) error = nil", group)
		}
	}
}

func TestCgroupV2ResolveRootUsesDelegatedSubtree(t *testing.T) {
	driver := &cgroupV2Driver{mountpoint: "/sys/fs/cgroup", delegationGroup: "/system.slice/axnoded.service"}
	got, err := driver.ResolveRoot("sandbox")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/system.slice/axnoded.service/sandbox" {
		t.Fatalf("ResolveRoot() = %q", got)
	}
}
