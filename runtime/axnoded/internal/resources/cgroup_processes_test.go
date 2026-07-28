package resources

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func TestKillCgroupProcesses(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("Requires root to create cgroups")
	}
	const cgroupPath = "test-kill-runp"
	cgroupDriver, err := os2.DefaultCgroupDriver()
	assert.NoError(t, err)
	cgroup, err := cgroupDriver.Create(cgroupPath, &specs.LinuxResources{})
	assert.NoError(t, err)

	cmd := exec.Command("sleep", "666")
	err = cmd.Start()
	assert.NoError(t, err)

	err = cgroup.AddProc(uint64(cmd.Process.Pid))
	if err != nil && (strings.Contains(err.Error(), "operation not supported") || strings.Contains(err.Error(), "permission denied")) {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Skipf("cgroup process migration is not supported in this environment: %v", err)
	}
	assert.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	err = killCgroupProcesses(cgroupPath)
	assert.NoError(t, err)

	select {
	case <-time.After(1 * time.Second):
		assert.Fail(t, "timeout")
	case err := <-done:
		assert.ErrorContains(t, err, "signal: killed")
	}

	err = killCgroupProcesses("/non-exist-test-cgroup-path")
	assert.Error(t, err)
}
