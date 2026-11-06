//go:build linux

package hostlinux

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
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

func TestStoppedCgroupRejectsCrossBootAutomaticRecovery(t *testing.T) {
	stopped, err := RunscCgroupStopped("/absent/allocation/workload", os.Getpid(), "previous-kernel-boot", "old-mount", 1)
	require.ErrorContains(t, err, "previous kernel boot")
	require.False(t, stopped, "a current-boot PID must not be killed for a previous-boot allocation")
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
