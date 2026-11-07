//go:build !linux

package hostlinux

import "fmt"

func KillRunscCgroup(string, int, string, string, string, uint64) error {
	return fmt.Errorf("sandbox cgroup fail-stop requires Linux")
}

func RunscCgroupStopped(string, int, string, string, uint64) (bool, error) {
	return false, fmt.Errorf("sandbox cgroup fail-stop requires Linux")
}

func ConformanceCgroupStopped(string, int) (bool, error) {
	return false, fmt.Errorf("conformance cgroup termination verification requires Linux")
}
