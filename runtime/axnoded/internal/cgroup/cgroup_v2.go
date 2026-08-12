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
	"syscall"

	cg2 "github.com/containerd/cgroups/v3/cgroup2"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

type cgroupV2Driver struct {
	mountpoint      string
	delegationGroup string
}

func (d *cgroupV2Driver) Mode() string { return CgroupModeV2 }

func (d *cgroupV2Driver) ResolveRoot(rootName string) (string, error) {
	name, err := validateManagedRootName(rootName)
	if err != nil {
		return "", err
	}
	delegation := normalizeGroup(d.delegationGroup)
	return normalizeGroup(path.Join(delegation, name)), nil
}

func (d *cgroupV2Driver) EnsureRoot(rootName string) error {
	rootName, err := d.ResolveRoot(rootName)
	if err != nil {
		return err
	}
	delegationDir := path.Join(d.mountpoint, trimGroup(d.delegationGroup))
	if err := requireCgroupController(delegationDir, "memory"); err != nil {
		return fmt.Errorf("validate delegated memory controller: %w", err)
	}
	internalDir := path.Join(delegationDir, cgroupInternalGroup)
	if err := stdos.MkdirAll(internalDir, 0755); err != nil {
		return err
	}
	if err := d.moveProcessesToChild(delegationDir, internalDir); err != nil {
		return fmt.Errorf("move delegated control processes to internal cgroup: %w", err)
	}
	if err := d.enableControllers(delegationDir); err != nil {
		return fmt.Errorf("enable delegated root controllers: %w", err)
	}
	sandboxDir := path.Join(d.mountpoint, trimGroup(rootName))
	if err := stdos.MkdirAll(sandboxDir, 0755); err != nil {
		return err
	}
	return d.enableControllers(sandboxDir)
}

func (d *cgroupV2Driver) Create(group string, resources *specs.LinuxResources) (Cgroup, error) {
	_ = resources
	group, parentDir, err := d.managedGroup(group, false)
	if err != nil {
		return nil, err
	}
	if err := stdos.MkdirAll(parentDir, 0755); err != nil {
		return nil, err
	}
	if err := d.ensureControllersPath(parentDir); err != nil {
		return nil, err
	}

	workloadDir := path.Join(parentDir, CgroupWorkloadLeafName)
	if err := stdos.MkdirAll(workloadDir, 0755); err != nil {
		return nil, err
	}

	manager, err := cg2.Load(WorkloadGroup(group, d.Mode()), cg2.WithMountpoint(d.mountpoint))
	if err != nil {
		return nil, err
	}
	return &cgroupV2{manager: manager}, nil
}

func (d *cgroupV2Driver) ensureControllersPath(dir string) error {
	dir = filepath.Clean(dir)
	delegationDir := filepath.Clean(path.Join(d.mountpoint, trimGroup(d.delegationGroup)))
	relative, err := filepath.Rel(delegationDir, dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("cgroup dir %s is outside delegated root %s", dir, delegationDir)
	}

	var chain []string
	for current := dir; ; current = path.Dir(current) {
		chain = append(chain, current)
		if current == delegationDir {
			break
		}
	}

	for i := len(chain) - 1; i >= 0; i-- {
		if err := d.ensureControllers(chain[i]); err != nil {
			return err
		}
	}
	return nil
}

func (d *cgroupV2Driver) ensureControllers(dir string) error {
	currentDir, currentErr := d.currentCgroupDir()
	if currentErr == nil && filepath.Clean(dir) == filepath.Clean(currentDir) {
		processCount, countErr := cgroupProcessCount(dir)
		if countErr == nil && processCount > 0 {
			logrus.Debugf("evacuate current cgroup %s with %d processes before enabling controllers", dir, processCount)
			if moveErr := d.moveCurrentProcessesToChild(dir); moveErr != nil {
				return moveErr
			}
		}
	}

	err := d.enableControllers(dir)
	if err == nil || !isCgroupBusyError(err) {
		return err
	}

	currentDir, currentErr = d.currentCgroupDir()
	if currentErr != nil {
		return err
	}
	if filepath.Clean(dir) != filepath.Clean(currentDir) {
		return err
	}

	logrus.Warnf("cgroup subtree_control busy on %s, moving current cgroup processes into %s before retry", dir, path.Join(dir, cgroupInternalGroup))
	if moveErr := d.moveCurrentProcessesToChild(dir); moveErr != nil {
		return fmt.Errorf("%w: evacuate current cgroup failed: %v", err, moveErr)
	}
	return d.enableControllers(dir)
}

func (d *cgroupV2Driver) enableControllers(dir string) error {
	controllersData, err := stdos.ReadFile(path.Join(dir, "cgroup.controllers"))
	if err != nil {
		return err
	}
	enabledData, err := stdos.ReadFile(path.Join(dir, "cgroup.subtree_control"))
	if err != nil {
		return err
	}

	available := strings.Fields(string(controllersData))
	enabled := make(map[string]bool, len(strings.Fields(string(enabledData))))
	for _, item := range strings.Fields(string(enabledData)) {
		enabled[item] = true
	}

	for _, controller := range available {
		if enabled[controller] {
			continue
		}
		if err := writeCgroupControl(path.Join(dir, "cgroup.subtree_control"), "+"+controller); err != nil {
			if strings.Contains(err.Error(), "operation not supported") {
				continue
			}
			return err
		}
	}
	return nil
}

func requireCgroupController(dir, controller string) error {
	data, err := stdos.ReadFile(path.Join(dir, "cgroup.controllers"))
	if err != nil {
		return err
	}
	for _, available := range strings.Fields(string(data)) {
		if available == controller {
			return nil
		}
	}
	return fmt.Errorf("controller %q is not delegated through %s", controller, dir)
}

func (d *cgroupV2Driver) managedGroup(group string, allowWorkload bool) (string, string, error) {
	group = normalizeGroup(group)
	delegation := normalizeGroup(d.delegationGroup)
	relative, err := filepath.Rel(delegation, group)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("cgroup %q is outside delegated root %q", group, delegation)
	}
	first := strings.Split(relative, string(filepath.Separator))[0]
	if first == cgroupInternalGroup {
		return "", "", fmt.Errorf("cgroup %q belongs to the reserved internal domain", group)
	}
	if !allowWorkload && path.Base(group) == CgroupWorkloadLeafName {
		return "", "", fmt.Errorf("allocation operation requires a parent cgroup, got workload leaf %q", group)
	}
	return group, path.Join(d.mountpoint, trimGroup(group)), nil
}

func (d *cgroupV2Driver) currentCgroupDir() (string, error) {
	group, err := currentUnifiedGroup()
	if err != nil {
		return "", err
	}
	return path.Join(d.mountpoint, trimGroup(group)), nil
}

func currentUnifiedGroup() (string, error) {
	data, err := stdos.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" {
			return normalizeGroup(parts[2]), nil
		}
	}
	return "", fmt.Errorf("unified cgroup path not found in /proc/self/cgroup")
}

func (d *cgroupV2Driver) moveCurrentProcessesToChild(dir string) error {
	internalDir := path.Join(dir, cgroupInternalGroup)
	if err := stdos.MkdirAll(internalDir, 0755); err != nil {
		return err
	}
	return d.moveProcessesToChild(dir, internalDir)
}

func (d *cgroupV2Driver) moveProcessesToChild(dir, internalDir string) error {
	data, err := stdos.ReadFile(path.Join(dir, "cgroup.procs"))
	if err != nil {
		return err
	}
	target := path.Join(internalDir, "cgroup.procs")
	for _, pid := range strings.Fields(string(data)) {
		if err := writeCgroupControl(target, pid); err != nil {
			if errors.Is(err, syscall.ESRCH) {
				continue
			}
			return err
		}
	}
	return nil
}

func cgroupProcessCount(dir string) (int, error) {
	data, err := stdos.ReadFile(path.Join(dir, "cgroup.procs"))
	if err != nil {
		return 0, err
	}
	return len(strings.Fields(string(data))), nil
}

func writeCgroupControl(path, value string) error {
	f, err := stdos.OpenFile(path, stdos.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(value)
	return err
}

func isCgroupBusyError(err error) bool {
	return errors.Is(err, syscall.EBUSY) || strings.Contains(err.Error(), "device or resource busy")
}

func (d *cgroupV2Driver) Load(group string) (Cgroup, error) {
	group, _, err := d.managedGroup(group, true)
	if err != nil {
		return nil, err
	}
	manager, err := cg2.Load(group, cg2.WithMountpoint(d.mountpoint))
	if err != nil {
		return nil, err
	}
	return &cgroupV2{manager: manager}, nil
}

func (d *cgroupV2Driver) ExistingGroups(rootName string) ([]string, error) {
	rootName, rootDir, err := d.managedGroup(rootName, false)
	if err != nil {
		return nil, err
	}
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

func (d *cgroupV2Driver) Remove(group string) error {
	group, parentDir, err := d.managedGroup(group, false)
	if err != nil {
		return err
	}
	workloadDir := path.Join(parentDir, CgroupWorkloadLeafName)
	for _, dir := range []string{workloadDir, parentDir} {
		if err := stdos.Remove(dir); err != nil && !stdos.IsNotExist(err) {
			// An unexpected child or remaining process is durable cleanup debt.
			// Never recursively erase a cgroup subtree whose ownership was not
			// established by this allocation lease.
			return fmt.Errorf("remove cgroup %s: %w", dir, err)
		}
	}
	return nil
}

func (d *cgroupV2Driver) LocalCPUCount() (int, error) {
	delegationDir := path.Join(d.mountpoint, trimGroup(d.delegationGroup))
	data, err := stdos.ReadFile(path.Join(delegationDir, "cpu.max"))
	if err != nil {
		return 0, fmt.Errorf("read cpu.max failed: %w", err)
	}
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) != 2 {
		return 0, fmt.Errorf("unexpected cpu.max format: %q", strings.TrimSpace(string(data)))
	}
	if fields[0] == "max" {
		return cpuCountFromCpusetFiles(
			path.Join(delegationDir, "cpuset.cpus"),
			path.Join(delegationDir, "cpuset.cpus.effective"),
		)
	}
	quota, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, err
	}
	period, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, err
	}
	if period == 0 {
		return 0, errors.New("cpu.max period is 0")
	}
	return maxInt(1, quota/period), nil
}

type cgroupV2 struct {
	manager *cg2.Manager
}

func (c *cgroupV2) Update(resources *specs.LinuxResources) error {
	converted := cg2.ToResources(resources)
	swap, err := cgroupV2SwapLimit(resources.Memory)
	if err != nil {
		return err
	}
	if resources.Memory != nil && resources.Memory.Swap != nil {
		converted.Memory.Swap = swap
	}
	return c.manager.Update(converted)
}

func (c *cgroupV2) Delete() error {
	return c.manager.Delete()
}

func (c *cgroupV2) Stats() (*CgroupStats, error) {
	metrics, err := c.manager.Stat()
	if err != nil {
		return nil, err
	}
	stats := &CgroupStats{}
	if metrics.GetCPU() != nil {
		stats.CPUUsageTotal = metrics.GetCPU().GetUsageUsec() * 1000
		stats.CPUUsageKernel = metrics.GetCPU().GetSystemUsec() * 1000
		stats.CPUUsageUser = metrics.GetCPU().GetUserUsec() * 1000
	}
	if metrics.GetMemory() != nil {
		stats.MemoryUsage = metrics.GetMemory().GetUsage()
		stats.MemoryLimit = metrics.GetMemory().GetUsageLimit()
		stats.MemoryMaxUsage = metrics.GetMemory().GetUsage()
	}
	return stats, nil
}

func (c *cgroupV2) AddProc(pid uint64) error {
	return c.manager.AddProc(pid)
}

func (c *cgroupV2) Processes(recursive bool) ([]int, error) {
	procs, err := c.manager.Procs(recursive)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(procs))
	for _, proc := range procs {
		ids = append(ids, int(proc))
	}
	return ids, nil
}
