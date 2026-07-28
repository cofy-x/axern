package resources

import (
	"sync"

	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

var fetchCPU sync.Once
var cpuNum int
var cpuErr error

func cpuMemorySubsystems() ([]string, error) {
	return []string{"cpu", "cpuset", "memory"}, nil
}

func getLocalCpuNum() (int, error) {
	if cpuNum > 0 {
		return cpuNum, nil
	}
	fetchCPU.Do(func() {
		var driver os2.CgroupDriver
		driver, cpuErr = os2.DefaultCgroupDriver()
		if cpuErr != nil {
			return
		}
		cpuNum, cpuErr = driver.LocalCPUCount()
	})
	return cpuNum, cpuErr
}
