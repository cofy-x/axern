//go:build linux

package resources

import (
	"fmt"
	"syscall"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

func killCgroupProcesses(cgroupPath string) error {
	driver, err := os2.DefaultCgroupDriver()
	if err != nil {
		return fmt.Errorf("get cgroup driver failed: %v", err)
	}
	cgroup, err := driver.Load(cgroupPath)
	if err != nil {
		return fmt.Errorf("load cgroup failed: %v", err)
	}
	processes, err := cgroup.Processes(true)
	if err != nil {
		return fmt.Errorf("getting processes failed: %v", err)
	}
	for _, pid := range processes {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}

	return nil
}
