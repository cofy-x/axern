//go:build linux

package hostlinux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

func IsCgroupWritePermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "/sys/fs/cgroup") && strings.Contains(msg, "permission denied")
}

func cloneResource(resource *runtimeapi.LinuxContainerResources) *runtimeapi.LinuxContainerResources {
	if resource == nil {
		return nil
	}
	return proto.Clone(resource).(*runtimeapi.LinuxContainerResources)
}

func hasCPUResource(resource *runtimeapi.LinuxContainerResources) bool {
	return resource != nil && (resource.CpuShares > 0 || resource.CpuQuota > 0 || resource.CpuPeriod > 0 || resource.CpusetCpus != "" || resource.CpusetMems != "")
}

func hasMemoryResource(resource *runtimeapi.LinuxContainerResources) bool {
	return resource != nil && (resource.MemoryLimitInBytes > 0 || resource.MemorySwapLimitInBytes > 0)
}

func resourceDirForCgroupPath(cgroupPath string) string {
	return filepath.Join("/sys/fs/cgroup", strings.TrimPrefix(filepath.Clean("/"+cgroupPath), "/"))
}

func RuntimeCgroupPath(driver os2.CgroupDriver, cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}
	mode := ""
	if driver != nil {
		mode = driver.Mode()
	}
	return os2.WorkloadGroup(cgroupPath, mode)
}

func SanitizeResourceForDriver(driver os2.CgroupDriver, cgroupPath string, resource *runtimeapi.LinuxContainerResources) *runtimeapi.LinuxContainerResources {
	if resource == nil {
		return nil
	}
	if driver == nil || driver.Mode() != os2.CgroupModeV2 {
		return resource
	}

	sanitized := cloneResource(resource)
	resourceDir := resourceDirForCgroupPath(cgroupPath)
	if hasCPUResource(sanitized) {
		if _, err := os.Stat(filepath.Join(resourceDir, "cpu.weight")); err != nil {
			sanitized.CpuShares = 0
			sanitized.CpuQuota = 0
			sanitized.CpuPeriod = 0
			sanitized.CpusetCpus = ""
			sanitized.CpusetMems = ""
		}
	}
	if hasMemoryResource(sanitized) {
		if _, err := os.Stat(filepath.Join(resourceDir, "memory.max")); err != nil {
			sanitized.MemoryLimitInBytes = 0
			sanitized.MemorySwapLimitInBytes = 0
		}
	}

	if !hasCPUResource(sanitized) && !hasMemoryResource(sanitized) && len(sanitized.HugepageLimits) == 0 && len(sanitized.Unified) == 0 {
		return nil
	}
	return sanitized
}

func UpdateCgroup(cgroupPath string, resource *runtimeapi.LinuxContainerResources) error {
	if resource == nil {
		return nil
	}

	driver, err := os2.DefaultCgroupDriver()
	if err != nil {
		return err
	}
	cgroup, err := driver.Load(cgroupPath)
	if err != nil {
		return err
	}

	var cpu spec.LinuxCPU
	if resource.CpuShares > 0 {
		cpu.Shares = &resource.CpuShares
	}
	if resource.CpuQuota > 0 {
		cpu.Quota = &resource.CpuQuota
	}
	if resource.CpuPeriod > 0 {
		cpu.Period = &resource.CpuPeriod
	}
	if resource.CpusetCpus != "" {
		cpu.Cpus = resource.CpusetCpus
	}
	if resource.CpusetMems != "" {
		cpu.Mems = resource.CpusetMems
	}
	var mem spec.LinuxMemory
	if resource.MemoryLimitInBytes > 0 {
		mem.Limit = &resource.MemoryLimitInBytes
	}
	if resource.MemorySwapLimitInBytes > 0 {
		mem.Swap = &resource.MemorySwapLimitInBytes
	}

	cgroupResource := &spec.LinuxResources{
		CPU:    &cpu,
		Memory: &mem,
	}

	return cgroup.Update(cgroupResource)
}

func IsPathReadOnly(path string) (bool, error) {
	if path == "" {
		return false, fmt.Errorf("path is empty")
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return false, fmt.Errorf("statfs %s: %w", path, err)
	}
	return stat.Flags&unix.ST_RDONLY != 0, nil
}

func isXFSMounted(dir string) (bool, error) {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false, fmt.Errorf("read /proc/mounts: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[1] == dir && fields[2] == "xfs" {
			return true, nil
		}
	}
	return false, nil
}

func EnsureXFSMount(filestoreDir, size string) error {
	if err := os.MkdirAll(filestoreDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %v", filestoreDir, err)
	}

	imgFile := filepath.Join(filepath.Dir(filestoreDir), "xfs.img")

	mounted, err := isXFSMounted(filestoreDir)
	if err != nil {
		return err
	}
	if mounted {
		return nil
	}

	_ = os.Remove(imgFile)

	if out, err := exec.Command("truncate", "-s", size, imgFile).CombinedOutput(); err != nil {
		return fmt.Errorf("truncate %s: %s: %v", imgFile, out, err)
	}
	out, err := exec.Command("mkfs.xfs", "-f", "-m", "reflink=1", "-i", "nrext64=0", imgFile).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "unknown option") && strings.Contains(string(out), "nrext64") {
			out, err = exec.Command("mkfs.xfs", "-f", "-m", "reflink=1", imgFile).CombinedOutput()
			if err != nil {
				_ = os.Remove(imgFile)
				return fmt.Errorf("mkfs.xfs: %s: %v", out, err)
			}
		} else {
			_ = os.Remove(imgFile)
			return fmt.Errorf("mkfs.xfs: %s: %v", out, err)
		}
	}

	if out, err := exec.Command("mount", "-o", "loop,defaults,discard", imgFile, filestoreDir).CombinedOutput(); err != nil {
		_ = os.Remove(imgFile)
		return fmt.Errorf("mount xfs %s -> %s: %s: %v", imgFile, filestoreDir, out, err)
	}

	return nil
}

func CleanupXFSMount(filestoreDir string) error {
	if filestoreDir == "" {
		return nil
	}
	mounted, err := isXFSMounted(filestoreDir)
	if err != nil {
		return err
	}
	if !mounted {
		return nil
	}
	if out, err := exec.Command("umount", filestoreDir).CombinedOutput(); err != nil {
		return fmt.Errorf("umount %s: %s: %v", filestoreDir, out, err)
	}
	imgFile := filepath.Join(filepath.Dir(filestoreDir), "xfs.img")
	_ = os.Remove(imgFile)
	_ = os.Remove(filestoreDir)
	return nil
}
