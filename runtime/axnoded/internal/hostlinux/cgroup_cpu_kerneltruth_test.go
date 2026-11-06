//go:build linux && kerneltruth

package hostlinux

import (
	"os"
	"path/filepath"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestCgroupCPUControlsKernelTruth(t *testing.T) {
	require.Equal(t, 0, os.Geteuid(), "kernel truth requires root in an isolated writable delegated cgroup-v2 scope")
	driver, err := cgroup.DefaultCgroupDriver()
	require.NoError(t, err)
	require.Equal(t, cgroup.CgroupModeV2, driver.Mode())
	group, err := driver.ResolveRoot(filepath.Base(filepath.Dir(t.TempDir())))
	require.NoError(t, err)
	// The driver may evacuate the caller to enable controllers. Run this test
	// only in its own delegated scope or disposable private cgroup namespace.
	// Removal is restricted to this test's unique empty allocation group.
	t.Cleanup(func() { require.NoError(t, driver.Remove(group)) })
	_, err = driver.Create(group, &specs.LinuxResources{})
	require.NoError(t, err)
	leaf := RuntimeCgroupPath(driver, group)
	resource := &apipb.LinuxContainerResources{CpuShares: 256, CpuQuota: 25000, CpuPeriod: 100000}
	require.NoError(t, UpdateCgroup(leaf, resource))
	require.NoError(t, VerifyCgroupCPUControls(leaf, resource))
	leafDir := resourceDirForCgroupPath(leaf)

	for _, value := range []string{"max 100000", "50000 100000", "25000 200000"} {
		require.NoError(t, writeCgroupFile(filepath.Join(leafDir, "cpu.max"), value))
		require.Error(t, VerifyCgroupCPUControls(leaf, resource), "must reject changed kernel quota/period: %s", value)
	}
	require.NoError(t, UpdateCgroup(leaf, resource))
	require.NoError(t, writeCgroupFile(filepath.Join(leafDir, "cpu.weight"), "10000"))
	require.Error(t, VerifyCgroupCPUControls(leaf, resource), "must reject changed kernel weight")
	require.NoError(t, UpdateCgroup(leaf, resource))
	require.NoError(t, VerifyCgroupCPUControls(leaf, resource))

	// Disable CPU only beneath this empty test-owned allocation parent. A
	// request must not become unconstrained when the controller disappears.
	control := filepath.Join(resourceDirForCgroupPath(group), "cgroup.subtree_control")
	require.NoError(t, writeCgroupFile(control, "-cpu"))
	_, err = os.Stat(filepath.Join(leafDir, "cpu.max"))
	require.True(t, os.IsNotExist(err), "expected CPU controller removal, got %v", err)
	require.Error(t, VerifyCgroupCPUControls(leaf, resource))
	require.NoError(t, writeCgroupFile(control, "+cpu"))
	require.NoError(t, UpdateCgroup(leaf, resource))
	require.NoError(t, VerifyCgroupCPUControls(leaf, resource))
	require.NoError(t, driver.Remove(group))
	_, err = os.Stat(leafDir)
	require.True(t, os.IsNotExist(err), "test allocation cgroup must be removed")
}
