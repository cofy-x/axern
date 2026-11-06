//go:build linux && kerneltruth

package hostlinux

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
)

func TestCgroupPIDDomainKernelTruth(t *testing.T) {
	require.Equal(t, 0, os.Geteuid(), "kernel truth requires root in an isolated writable delegated cgroup-v2 scope")
	driver, err := cgroup.DefaultCgroupDriver()
	require.NoError(t, err)
	require.Equal(t, cgroup.CgroupModeV2, driver.Mode())
	group, err := driver.ResolveRoot(filepath.Base(filepath.Dir(t.TempDir())))
	require.NoError(t, err)
	// Controller setup may evacuate the caller. Run only in a dedicated
	// delegated scope or disposable private cgroup namespace. This test never
	// moves the test runner into the task-limited allocation subtree.
	t.Cleanup(func() { require.NoError(t, driver.Remove(group)) })
	_, err = driver.Create(group, &specs.LinuxResources{})
	require.NoError(t, err)
	leaf := RuntimeCgroupPath(driver, group)
	parentDir, leafDir := resourceDirForCgroupPath(group), resourceDirForCgroupPath(leaf)
	require.NoError(t, ConfigureCgroupPIDDomain(group, leaf, 64))
	require.NoError(t, VerifyCgroupPIDDomain(group, leaf, 64))

	for _, dir := range []string{parentDir, leafDir} {
		for _, value := range []string{"max", "63"} {
			require.NoError(t, writeCgroupFile(filepath.Join(dir, "pids.max"), value))
			require.Error(t, VerifyCgroupPIDDomain(group, leaf, 64), "changed host task ceiling must fail closed")
			require.NoError(t, ConfigureCgroupPIDDomain(group, leaf, 64))
		}
	}
	control := filepath.Join(parentDir, "cgroup.subtree_control")
	require.NoError(t, writeCgroupFile(control, "-pids"))
	_, err = os.Stat(filepath.Join(leafDir, "pids.max"))
	require.True(t, os.IsNotExist(err), "expected PID controller removal, got %v", err)
	require.Error(t, VerifyCgroupPIDDomain(group, leaf, 64))
	require.Error(t, ConfigureCgroupPIDDomain(group, leaf, 64))
	require.NoError(t, writeCgroupFile(control, "+pids"))
	require.NoError(t, ConfigureCgroupPIDDomain(group, leaf, 64))

	t.Run("parent bounds forks across child cgroups", func(t *testing.T) {
		require.NoError(t, ConfigureCgroupPIDDomain(group, leaf, 4))
		siblingDir := filepath.Join(parentDir, "fork-probe")
		require.NoError(t, os.Mkdir(siblingDir, 0o700))
		t.Cleanup(func() { require.NoError(t, os.Remove(siblingDir)) })
		leafFD, err := os.Open(leafDir)
		require.NoError(t, err)
		defer leafFD.Close()
		siblingFD, err := os.Open(siblingDir)
		require.NoError(t, err)
		defer siblingFD.Close()
		before, err := readInt64KeyValueFile(filepath.Join(leafDir, "pids.events"))
		require.NoError(t, err)
		for _, fd := range []*os.File{leafFD, leafFD, siblingFD, siblingFD} {
			cmd := exec.Command("sleep", "600")
			cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(fd.Fd())}
			require.NoError(t, cmd.Start())
			t.Cleanup(func() {
				require.NoError(t, cmd.Process.Kill())
				var exit *exec.ExitError
				require.ErrorAs(t, cmd.Wait(), &exit)
			})
		}
		current, err := readCgroupInt64(filepath.Join(parentDir, "pids.current"))
		require.NoError(t, err)
		require.Equal(t, int64(4), current)
		leafCurrent, err := readCgroupInt64(filepath.Join(leafDir, "pids.current"))
		require.NoError(t, err)
		require.Equal(t, int64(2), leafCurrent, "leaf has spare capacity; the parent must enforce the aggregate")
		cmd := exec.Command("sleep", "600")
		cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(leafFD.Fd())}
		err = cmd.Start()
		if err == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		require.ErrorIs(t, err, syscall.EAGAIN, "fork must fail when the parent host-task ceiling is reached")
		// The event is reported on the forking task's leaf even when an
		// ancestor supplied the exhausted ceiling.
		after, err := readInt64KeyValueFile(filepath.Join(leafDir, "pids.events"))
		require.NoError(t, err)
		require.Greater(t, after["max"], before["max"])
	})

	t.Run("threads consume the same task ceiling", func(t *testing.T) {
		require.NoError(t, ConfigureCgroupPIDDomain(group, leaf, 32))
		leafFD, err := os.Open(leafDir)
		require.NoError(t, err)
		defer leafFD.Close()
		before, err := readInt64KeyValueFile(filepath.Join(leafDir, "pids.events"))
		require.NoError(t, err)
		executable, err := os.Executable()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCgroupPIDThreadLimitHelper$")
		cmd.Env = append(os.Environ(), "AXERN_HOST_PID_THREAD_PROBE=1", "GOMAXPROCS=2")
		cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(leafFD.Fd())}
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		err = cmd.Run()
		require.NoError(t, ctx.Err(), "thread probe timed out: %s", output.String())
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, "thread probe must exhaust the kernel task ceiling: %s", output.String())
		require.NotEqual(t, 99, exit.ExitCode(), "thread probe exceeded the task ceiling")
		require.Contains(t, output.String(), "failed to create new OS thread")
		after, err := readInt64KeyValueFile(filepath.Join(leafDir, "pids.events"))
		require.NoError(t, err)
		require.Greater(t, after["max"], before["max"])
	})

	current, err := readCgroupInt64(filepath.Join(parentDir, "pids.current"))
	require.NoError(t, err)
	require.Zero(t, current, "all probe processes and threads must be reaped")
	require.NoError(t, driver.Remove(group))
	_, err = os.Stat(parentDir)
	require.True(t, errors.Is(err, os.ErrNotExist), "test allocation cgroup must be removed")
}

func TestCgroupPIDThreadLimitHelper(t *testing.T) {
	if os.Getenv("AXERN_HOST_PID_THREAD_PROBE") != "1" {
		return
	}
	// A fixed attempt count keeps this probe bounded even if the controller
	// fails. Each parked locked goroutine consumes a distinct Linux task.
	for range 64 {
		started := make(chan struct{})
		go func() {
			runtime.LockOSThread()
			close(started)
			select {}
		}()
		<-started
	}
	os.Exit(99)
}
