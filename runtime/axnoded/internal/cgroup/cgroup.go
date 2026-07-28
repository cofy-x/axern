package cgroup

import (
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"path/filepath"
	"strings"
	"sync"
)

type CgroupStats struct {
	CPUUsageTotal  uint64
	CPUUsageKernel uint64
	CPUUsageUser   uint64

	MemoryUsage    uint64
	MemoryLimit    uint64
	MemoryMaxUsage uint64
}

type Cgroup interface {
	Update(resources *specs.LinuxResources) error
	Delete() error
	Stats() (*CgroupStats, error)
	AddProc(pid uint64) error
	Processes(recursive bool) ([]int, error)
}

type CgroupDriver interface {
	Mode() string
	Create(group string, resources *specs.LinuxResources) (Cgroup, error)
	Load(group string) (Cgroup, error)
	ExistingGroups(rootName string) ([]string, error)
	Remove(group string) error
	LocalCPUCount() (int, error)
}

const (
	CgroupModeV1 = "v1"
	CgroupModeV2 = "v2"

	CgroupWorkloadLeafName = "workload"
	cgroupInternalGroup    = ".axnoded-system"
)

var (
	driverOnce sync.Once
	driverInst CgroupDriver
	driverErr  error
)

func DefaultCgroupDriver() (CgroupDriver, error) {
	driverOnce.Do(func() {
		driverInst, driverErr = newDefaultCgroupDriver()
	})
	return driverInst, driverErr
}

func ResetDefaultCgroupDriverForTest() {
	driverOnce = sync.Once{}
	driverInst = nil
	driverErr = nil
}

func normalizeGroup(group string) string {
	if group == "" {
		return "/"
	}
	group = filepath.Clean("/" + strings.TrimPrefix(group, "/"))
	if group == "." {
		return "/"
	}
	return group
}

func trimGroup(group string) string {
	return strings.TrimPrefix(normalizeGroup(group), "/")
}

func WorkloadGroup(group string, mode string) string {
	if mode != CgroupModeV2 {
		return normalizeGroup(group)
	}
	return filepath.Join(normalizeGroup(group), CgroupWorkloadLeafName)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
