//go:build !linux

package resources

import "fmt"

func staleDelegationCgroupProcesses(cgroupPath, _ string) ([]int, error) {
	return nil, fmt.Errorf("stale cgroup %s cleanup requires Linux", cgroupPath)
}

func removeStaleDelegationCgroup(cgroupPath, _ string) error {
	return fmt.Errorf("stale cgroup %s cleanup requires Linux", cgroupPath)
}
