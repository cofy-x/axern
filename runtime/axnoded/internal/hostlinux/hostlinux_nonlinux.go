//go:build !linux

package hostlinux

import (
	"fmt"
	"runtime"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

func VerifyCgroupMemoryLimit(string, int64) error {
	return fmt.Errorf("cgroup enforcement requires Linux")
}
func ProbeCgroupMemoryLimit(string) error     { return fmt.Errorf("cgroup enforcement requires Linux") }
func VerifyPIDInCgroup(string, int) error     { return fmt.Errorf("cgroup enforcement requires Linux") }
func VerifyCgroupPIDs(string, int, int) error { return fmt.Errorf("cgroup enforcement requires Linux") }
func VerifyRunscCgroupProcesses(string, int, string) error {
	return fmt.Errorf("cgroup enforcement requires Linux")
}
func ReadCgroupMemoryBreakdown(string) (map[string]int64, error) {
	return nil, fmt.Errorf("cgroup memory statistics require Linux")
}

func unsupported(op string) error {
	return fmt.Errorf("%s is unsupported on %s", op, runtime.GOOS)
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

func PrepareFilestore(filestoreDir, mode, image string, loopbackSizeBytes, systemReserveBytes int64) error {
	if filestoreDir == "" {
		return nil
	}
	return unsupported("filestore setup")
}

func CleanupFilestore(filestoreDir, mode, image string) error {
	if filestoreDir == "" {
		return nil
	}
	return unsupported("filestore cleanup")
}

type FilestoreCapabilities struct {
	OverlayReady      bool
	EROFSReady        bool
	ProjectQuotaReady bool
	FilesystemType    string
	MountIdentity     string
	EROFSProbeError   string
}

func CurrentBootID() (string, error) {
	return "", fmt.Errorf("kernel boot ID requires Linux")
}

func ReadFilestoreCapabilities(string) (FilestoreCapabilities, error) {
	return FilestoreCapabilities{}, unsupported("filestore capabilities")
}

func DirectoryIdentity(string) (string, error) {
	return "", unsupported("directory identity")
}
