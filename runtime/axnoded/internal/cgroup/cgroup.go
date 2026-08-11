package cgroup

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	specs "github.com/opencontainers/runtime-spec/specs-go"
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
	// ResolveRoot returns the kernel-absolute sandbox subtree below the
	// process's delegated cgroup-v2 root.
	ResolveRoot(rootName string) (string, error)
	// EnsureRoot establishes the runtime-owned hierarchy and controller
	// delegation without creating an allocation cgroup.
	EnsureRoot(rootName string) error
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
	cgroupInternalGroup    = "internal"
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

func validateManagedRootName(rootName string) (string, error) {
	rootName = strings.TrimSpace(rootName)
	if rootName == "" {
		return "", fmt.Errorf("managed cgroup root name is required")
	}
	if rootName == "." || rootName == ".." || strings.ContainsAny(rootName, `/\\`) {
		return "", fmt.Errorf("managed cgroup root %q must be a single child name", rootName)
	}
	if rootName == cgroupInternalGroup || rootName == CgroupWorkloadLeafName {
		return "", fmt.Errorf("managed cgroup root %q is reserved", rootName)
	}
	return rootName, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
