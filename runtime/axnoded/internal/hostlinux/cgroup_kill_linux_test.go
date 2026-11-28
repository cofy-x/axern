//go:build linux

package hostlinux

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestKillRunscCgroupRejectsIncompleteIdentityBeforeSideEffects(t *testing.T) {
	for _, test := range []struct {
		path, boot, mount string
		inode             uint64
		pid               int
	}{
		{path: "/", boot: "boot", mount: "mount", inode: 1, pid: 1},
		{path: "/node/../workload", boot: "boot", mount: "mount", inode: 1, pid: 1},
		{path: "/node/workload", mount: "mount", inode: 1, pid: 1},
		{path: "/node/workload", boot: "boot", inode: 1, pid: 1},
		{path: "/node/workload", boot: "boot", mount: "mount", pid: 1},
		{path: "/node/workload", boot: "boot", mount: "mount", inode: 1},
	} {
		require.ErrorContains(t, KillRunscCgroup(test.path, test.pid, "/missing/runsc", test.boot, test.mount, test.inode), "requires complete")
	}
}

func TestStoppedCgroupCannotTreatReusedRecordedPIDAsExit(t *testing.T) {
	stopped, err := recordedSentryGone(os.Getpid())
	require.ErrorContains(t, err, "retains or reuses")
	require.False(t, stopped)
	stopped, err = recordedSentryGone(1 << 30)
	require.NoError(t, err)
	require.True(t, stopped)
}

func TestEmptyCgroupCannotRetireAnUnrelatedLiveRecordedPID(t *testing.T) {
	process := exec.Command("sleep", "30")
	require.NoError(t, process.Start())
	t.Cleanup(func() {
		_ = process.Process.Kill()
		_ = process.Wait()
	})
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cgroup.events"), []byte("populated 0\nfrozen 0\n"), 0o600))
	fd, err := unix.Open(dir, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, unix.Close(fd)) })
	stopped, err := cgroupStoppedAt(fd, process.Process.Pid)
	require.ErrorContains(t, err, "retains or reuses")
	require.False(t, stopped)
	require.NoError(t, process.Process.Signal(syscall.Signal(0)), "a cached PID must never authorize a signal or cleanup")
}

func TestStoppedCgroupRejectsMalformedBootIdentity(t *testing.T) {
	stopped, err := RunscCgroupStopped("/absent/allocation/workload", os.Getpid(), "previous-kernel-boot", "old-mount", 1)
	require.ErrorContains(t, err, "previous kernel boot")
	require.False(t, stopped, "a current-boot PID must not be killed for a previous-boot allocation")
}

func TestPreviousBootCgroupRequiresMissingParentAndNeverUsesCurrentPID(t *testing.T) {
	var fs unix.Statfs_t
	if err := unix.Statfs("/sys/fs/cgroup", &fs); err != nil || fs.Type != unix.CGROUP2_SUPER_MAGIC {
		t.Skip("requires cgroup v2")
	}
	prior := uuid.NewString()
	livePID := os.Getpid()
	leaf := "/axern-test-" + uuid.NewString() + "/workload"
	stopped, err := RunscCgroupStopped(leaf, livePID, prior, "admitted-mount", 1)
	require.NoError(t, err)
	require.True(t, stopped)
	require.Error(t, KillRunscCgroup(leaf, livePID, "/missing/runsc", prior, "admitted-mount", 1))
	require.NoError(t, syscall.Kill(livePID, 0))
	for _, path := range []string{"/workload", "/no-such-parent/unexpected-leaf", "/cgroup.controllers/workload"} {
		stopped, err := RunscCgroupStopped(path, livePID, prior, "admitted-mount", 1)
		require.Error(t, err, path)
		require.False(t, stopped)
	}
}

func TestConformanceStoppedRequiresExactPathAndDeadRecordedPID(t *testing.T) {
	for _, path := range []string{"", "/", "relative", "/node/../owned", " /node/owned"} {
		_, err := ConformanceCgroupStopped(path, 1<<30)
		require.Error(t, err)
	}
	const absent = "/axern-test-nonexistent-conformance-parent-52e7"
	stopped, err := ConformanceCgroupStopped(absent, os.Getpid())
	require.ErrorContains(t, err, "retains or reuses")
	require.False(t, stopped)
	stopped, err = ConformanceCgroupStopped(absent, 1<<30)
	require.NoError(t, err)
	require.True(t, stopped)
	_, err = ConformanceCgroupStopped(absent, 0)
	require.Error(t, err)
}
