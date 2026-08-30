//go:build linux

package cgroup

import (
	"errors"
	"fmt"
	stdos "os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	cg1 "github.com/containerd/cgroups/v3/cgroup1"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type cgroupV1Driver struct{}

func (d *cgroupV1Driver) Mode() string { return CgroupModeV1 }

func (d *cgroupV1Driver) ResolveRoot(rootName string) (string, error) {
	name, err := validateManagedRootName(rootName)
	if err != nil {
		return "", err
	}
	return normalizeGroup(name), nil
}

func (d *cgroupV1Driver) EnsureRoot(string, int64) error {
	return fmt.Errorf("allocation memory domains require unified cgroup v2")
}

func (d *cgroupV1Driver) Create(group string, resources *specs.LinuxResources) (Cgroup, error) {
	cg, err := cg1.New(cg1.StaticPath(normalizeGroup(group)), resources, cg1.WithHiearchy(cg1.Default))
	if err != nil {
		return nil, err
	}
	return &cgroupV1{cg: cg}, nil
}

func (d *cgroupV1Driver) Load(group string) (Cgroup, error) {
	cg, err := cg1.Load(cg1.StaticPath(normalizeGroup(group)), cg1.WithHiearchy(cg1.Default))
	if err != nil {
		return nil, err
	}
	return &cgroupV1{cg: cg}, nil
}

func (d *cgroupV1Driver) ExistingGroups(rootName string) ([]string, error) {
	rootDir := path.Join("/sys/fs/cgroup/memory", trimGroup(rootName))
	entries, err := stdos.ReadDir(rootDir)
	if err != nil {
		if stdos.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var groups []string
	for _, entry := range entries {
		if entry.IsDir() {
			groups = append(groups, filepath.Join("/", trimGroup(rootName), entry.Name()))
		}
	}
	return groups, nil
}

func (d *cgroupV1Driver) Remove(group string) error {
	cg, err := d.Load(group)
	if err == nil {
		if err = cg.Delete(); err == nil {
			return nil
		}
	}

	subsystems, subErr := cg1.Default()
	if subErr != nil {
		return subErr
	}

	var errs []string
	for _, subsystem := range subsystems {
		dir := path.Join("/sys/fs/cgroup", string(subsystem.Name()), trimGroup(group))
		if err := stdos.RemoveAll(dir); err != nil && !stdos.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("delete cgroup %s failed: %s", group, strings.Join(errs, "; "))
	}
	return nil
}

func (d *cgroupV1Driver) LocalCPUCount() (int, error) {
	quotaData, err := stdos.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	if err != nil {
		return 0, fmt.Errorf("read cpu.cfs_quota_us failed: %w", err)
	}
	periodData, err := stdos.ReadFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if err != nil {
		return 0, fmt.Errorf("read cpu.cfs_period_us failed: %w", err)
	}

	quota, err := strconv.Atoi(strings.TrimSpace(string(quotaData)))
	if err != nil {
		return 0, err
	}
	period, err := strconv.Atoi(strings.TrimSpace(string(periodData)))
	if err != nil {
		return 0, err
	}
	if quota == -1 {
		return cpuCountFromCpusetFiles(
			"/sys/fs/cgroup/cpuset/cpuset.cpus",
			"/sys/fs/cgroup/cpuset/cpuset.cpus.effective",
		)
	}
	if period == 0 {
		return 0, errors.New("cpu period is 0")
	}
	return maxInt(1, quota/period), nil
}

type cgroupV1 struct {
	cg cg1.Cgroup
}

func (c *cgroupV1) Update(resources *specs.LinuxResources) error {
	return c.cg.Update(resources)
}

func (c *cgroupV1) Delete() error {
	return c.cg.Delete()
}

func (c *cgroupV1) Stats() (*CgroupStats, error) {
	metrics, err := c.cg.Stat()
	if err != nil {
		return nil, err
	}
	stats := &CgroupStats{}
	if metrics.GetCPU() != nil && metrics.GetCPU().GetUsage() != nil {
		stats.CPUUsageTotal = metrics.GetCPU().GetUsage().GetTotal()
		stats.CPUUsageKernel = metrics.GetCPU().GetUsage().GetKernel()
		stats.CPUUsageUser = metrics.GetCPU().GetUsage().GetUser()
	}
	if metrics.GetMemory() != nil && metrics.GetMemory().GetUsage() != nil {
		stats.MemoryUsage = metrics.GetMemory().GetUsage().GetUsage()
		stats.MemoryLimit = metrics.GetMemory().GetUsage().GetLimit()
		stats.MemoryMaxUsage = metrics.GetMemory().GetUsage().GetMax()
	}
	return stats, nil
}

func (c *cgroupV1) AddProc(pid uint64) error {
	return c.cg.AddProc(pid)
}

func (c *cgroupV1) Processes(recursive bool) ([]int, error) {
	procs, err := c.cg.Processes(cg1.Memory, recursive)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(procs))
	for _, proc := range procs {
		ids = append(ids, int(proc.Pid))
	}
	return ids, nil
}
