//go:build !linux

package resources

import "fmt"

func killCgroupProcesses(cgroupPath string) error {
	return fmt.Errorf("cgroup process cleanup is unsupported on this platform for %s", cgroupPath)
}
