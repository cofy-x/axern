//go:build !linux

package hostlinux

import (
	"fmt"
	"runtime"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
)

type CgroupMemoryDomain struct {
	BootID        string
	MountIdentity string
	ParentInode   uint64
	LeafInode     uint64
	LimitBytes    int64
	SwapMaxBytes  int64
	OOMGroup      bool
	InitialEvents map[string]uint64
}

type CgroupMemoryObservation struct {
	CurrentBytes  int64
	PeakBytes     int64
	PeakAvailable bool
	SwapCurrent   int64
	Stat          map[string]int64
	Events        map[string]uint64
	PSIAvailable  bool
	PSISomeAvg10  float64
	PSIFullAvg10  float64
	PSISomeTotal  uint64
	PSIFullTotal  uint64
}

func VerifyCgroupMemoryLimit(string, int64) error {
	return fmt.Errorf("cgroup enforcement requires Linux")
}
func ProbeCgroupMemoryLimit(string) error     { return fmt.Errorf("cgroup enforcement requires Linux") }
func VerifyPIDInCgroup(string, int) error     { return fmt.Errorf("cgroup enforcement requires Linux") }
func VerifyCgroupPIDs(string, int, int) error { return fmt.Errorf("cgroup enforcement requires Linux") }
func VerifyRuncCgroupProcessTree(string, int) error {
	return fmt.Errorf("cgroup enforcement requires Linux")
}
func VerifyRunscCgroupProcesses(string, int, string) error {
	return fmt.Errorf("cgroup enforcement requires Linux")
}
func ReadCgroupMemoryBreakdown(string) (map[string]int64, error) {
	return nil, fmt.Errorf("cgroup memory statistics require Linux")
}
func ConfigureCgroupMemoryDomain(string, string, int64) (*CgroupMemoryDomain, error) {
	return nil, fmt.Errorf("cgroup enforcement requires Linux")
}
func InspectCgroupMemoryDomain(string, string) (*CgroupMemoryDomain, error) {
	return nil, fmt.Errorf("cgroup enforcement requires Linux")
}
func InspectCgroupMemoryParent(string) (*CgroupMemoryDomain, error) {
	return nil, fmt.Errorf("cgroup enforcement requires Linux")
}
func VerifyCgroupMemoryDomain(string, string, int64, string, string, uint64, uint64) error {
	return fmt.Errorf("cgroup enforcement requires Linux")
}
func ReadCgroupMemoryObservation(string) (*CgroupMemoryObservation, error) {
	return nil, fmt.Errorf("cgroup memory statistics require Linux")
}
func ReclaimCgroupMemory(string) error { return fmt.Errorf("cgroup memory reclaim requires Linux") }

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
