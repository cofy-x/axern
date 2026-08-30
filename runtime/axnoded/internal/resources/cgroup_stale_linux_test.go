//go:build linux

package resources

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	cmap "github.com/orcaman/concurrent-map/v2"
)

func TestStaleDelegationCgroupProcessesUsesOnlyOwnedHierarchy(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "old", "sandbox", "Abc123Def456")
	workload := filepath.Join(parent, "workload")
	if err := os.MkdirAll(workload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "cgroup.procs"), []byte("12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workload, "cgroup.procs"), []byte("34\n12\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	processes, err := staleDelegationCgroupProcessesAt(root, "/old/sandbox/Abc123Def456", "sandbox")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(processes)
	if !slices.Equal(processes, []int{12, 34}) {
		t.Fatalf("processes = %v, want [12 34]", processes)
	}
}

func TestStaleDelegationCgroupRejectsUnexpectedChild(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "old", "sandbox", "Abc123Def456")
	if err := os.MkdirAll(filepath.Join(parent, "unowned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := staleDelegationCgroupProcessesAt(root, "/old/sandbox/Abc123Def456", "sandbox"); err == nil {
		t.Fatal("stale cgroup process inventory accepted an unexpected child")
	}
}

func TestRemoveStaleDelegationCgroupRejectsDifferentRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "old", "shared", "Abc123Def456"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleDelegationCgroupAt(root, "/old/shared/Abc123Def456", "sandbox"); err == nil {
		t.Fatal("stale cgroup cleanup accepted a different managed root")
	}
}

func TestRemoveStaleDelegationCgroupDeletesOnlyKnownEmptyDirs(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "old", "sandbox", "Abc123Def456")
	if err := os.MkdirAll(filepath.Join(parent, "workload"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := removeStaleDelegationCgroupAt(root, "/old/sandbox/Abc123Def456", "sandbox"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("stale allocation cgroup still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "old", "sandbox")); err != nil {
		t.Fatalf("managed root ownership expanded beyond the allocation: %v", err)
	}
}

func TestStaleDelegationRetirementOnRealCgroupV2(t *testing.T) {
	if os.Getenv("AXERN_VERIFY_STALE_CGROUP_CLEANUP") != "1" {
		t.Skip("set AXERN_VERIFY_STALE_CGROUP_CLEANUP=1 in a delegated cgroup-v2 environment")
	}
	const limit = int64(64 << 20)
	pid := strconv.Itoa(os.Getpid())
	currentRootName := "axern-current-" + pid
	driver, err := os2.DefaultCgroupDriver()
	if err != nil {
		t.Fatal(err)
	}
	currentRootPath, err := driver.ResolveRoot(currentRootName)
	if err != nil {
		t.Fatal(err)
	}
	if err := driver.EnsureRoot(currentRootName, config.RuntimeConformanceMemoryMaxBytes); err != nil {
		t.Fatal(err)
	}
	currentRootDir := filepath.Join(cgroupFilesystemRoot, strings.TrimPrefix(currentRootPath, "/"))
	rootPath := filepath.Join(filepath.Dir(currentRootPath), "axern-stale-"+pid)
	rootDir := filepath.Join(cgroupFilesystemRoot, strings.TrimPrefix(rootPath, "/"))
	sandboxDir := filepath.Join(rootDir, currentRootName)
	parentPath := filepath.Join(rootPath, currentRootName, "Abc123Def456")
	parentDir := filepath.Join(cgroupFilesystemRoot, parentPath)
	workloadPath := filepath.Join(parentPath, "workload")
	workloadDir := filepath.Join(cgroupFilesystemRoot, workloadPath)
	t.Cleanup(func() {
		_ = os.Remove(workloadDir)
		_ = os.Remove(parentDir)
		_ = os.Remove(sandboxDir)
		_ = os.Remove(rootDir)
		_ = os.Remove(currentRootDir)
	})

	for _, dir := range []string{rootDir, sandboxDir, parentDir, workloadDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if dir != workloadDir {
			if err := writeExistingCgroupFile(filepath.Join(dir, "cgroup.subtree_control"), "+memory"); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, dir := range []string{parentDir, workloadDir} {
		if err := writeExistingCgroupFile(filepath.Join(dir, "memory.max"), strconv.FormatInt(limit, 10)); err != nil {
			t.Fatal(err)
		}
		if err := writeExistingCgroupFile(filepath.Join(dir, "memory.swap.max"), "0"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeExistingCgroupFile(filepath.Join(parentDir, "memory.oom.group"), "1"); err != nil {
		t.Fatal(err)
	}
	domain, err := hostlinux.InspectCgroupMemoryDomain(parentPath, workloadPath)
	if err != nil {
		t.Fatal(err)
	}
	lease := &apipb.CgroupLease{
		CgroupID:     parentPath,
		State:        apipb.CgroupLifecycleState_CGROUP_LIFECYCLE_STATE_RETIRING,
		AllocationID: "allocation-a", RuntimeName: "runc",
		MemoryRequestBytes: limit, MemoryLimitBytes: limit,
		AssignedAtUnixNano: time.Now().Add(-time.Minute).UnixNano(),
		RetiringAtUnixNano: time.Now().Add(-time.Second).UnixNano(),
		OwnerKind:          apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD,
		CgroupBootID:       domain.BootID, CgroupMountIdentity: domain.MountIdentity,
		CgroupParentInode: domain.ParentInode, CgroupLeafInode: domain.LeafInode,
	}
	manager := &CgroupManager{
		rootName: currentRootPath, leases: cmap.New[*apipb.CgroupLease](),
		retirementMemory: hostCgroupRetirementMemory{}, cgroupDriver: &stubCgroupDriver{},
	}
	manager.leases.Set(parentPath, lease)
	if err := manager.convergeRetiringCgroup(parentPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(parentDir); !os.IsNotExist(err) {
		t.Fatalf("retired stale cgroup still exists or stat failed: %v", err)
	}
}

func writeExistingCgroupFile(path, value string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(value); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
