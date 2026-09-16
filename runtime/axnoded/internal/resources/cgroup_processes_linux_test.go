//go:build linux && kerneltruth

package resources

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestKillCgroupProcesses(t *testing.T) {
	require.Equal(t, 0, os.Geteuid(), "kernel truth requires root in an isolated writable cgroup-v2 namespace")
	driver, err := cgroup.DefaultCgroupDriver()
	require.NoError(t, err)
	require.Equal(t, cgroup.CgroupModeV2, driver.Mode())
	group, err := driver.ResolveRoot(filepath.Base(filepath.Dir(t.TempDir())))
	require.NoError(t, err)
	// Register removal before Create, which can fail after partially creating
	// the hierarchy. Never remove anything outside this test's unique group.
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Join("/sys/fs/cgroup", group)); os.IsNotExist(err) {
			return
		}
		require.NoError(t, driver.Remove(group))
	})
	managed, err := driver.Create(group, &specs.LinuxResources{})
	require.NoError(t, err, "requires writable delegated cgroup controllers")
	cmd := exec.Command("sleep", "600")
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		err := cmd.Process.Kill()
		if err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Errorf("cleanup child: %v", err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("cleanup child did not reap")
		}
	})
	require.NoError(t, managed.AddProc(uint64(cmd.Process.Pid)))
	pids, err := managed.Processes(true)
	require.NoError(t, err)
	require.Contains(t, pids, cmd.Process.Pid)
	require.NoError(t, killCgroupProcesses(group))
	select {
	case err := <-done:
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit)
		status := exit.Sys().(syscall.WaitStatus)
		require.True(t, status.Signaled())
		require.Equal(t, syscall.SIGKILL, status.Signal())
	case <-time.After(5 * time.Second):
		t.Fatal("cgroup kill did not reap child")
	}
	require.NoError(t, killCgroupProcesses(group), "empty group cleanup must be idempotent")
	require.NoError(t, driver.Remove(group))
	require.Error(t, killCgroupProcesses(group), "removed group must not be treated as successfully loaded")
}
