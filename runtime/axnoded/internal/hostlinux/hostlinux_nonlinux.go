//go:build !linux

package hostlinux

import (
	"fmt"
	"runtime"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

func unsupported(op string) error {
	return fmt.Errorf("%s is unsupported on %s", op, runtime.GOOS)
}

func IsCgroupWritePermissionError(err error) bool {
	return false
}

func RuntimeCgroupPath(driver os2.CgroupDriver, cgroupPath string) string {
	if cgroupPath == "" {
		return ""
	}
	return os2.WorkloadGroup(cgroupPath, "")
}

func SanitizeResourceForDriver(driver os2.CgroupDriver, cgroupPath string, resource *runtimeapi.LinuxContainerResources) *runtimeapi.LinuxContainerResources {
	return resource
}

func UpdateCgroup(cgroupPath string, resource *runtimeapi.LinuxContainerResources) error {
	if resource == nil {
		return nil
	}
	return unsupported("cgroup update")
}

func IsPathReadOnly(path string) (bool, error) {
	return false, unsupported("path readonly detection")
}

func EnsureXFSMount(filestoreDir, size string) error {
	if filestoreDir == "" {
		return nil
	}
	return unsupported("xfs mount setup")
}

func CleanupXFSMount(filestoreDir string) error {
	if filestoreDir == "" {
		return nil
	}
	return unsupported("xfs mount cleanup")
}
