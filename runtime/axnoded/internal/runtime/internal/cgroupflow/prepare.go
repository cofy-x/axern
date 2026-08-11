package cgroupflow

import (
	"fmt"
	"strings"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

var (
	defaultCgroupDriver       = os2.DefaultCgroupDriver
	runtimeCgroupPath         = hostlinux.RuntimeCgroupPath
	sanitizeResourceForDriver = hostlinux.SanitizeResourceForDriver
	updateCgroup              = hostlinux.UpdateCgroup
	configureMemoryDomain     = hostlinux.ConfigureCgroupMemoryDomain
)

type RuntimePolicy struct {
	IgnoreCgroups           bool
	DropResourceWhenIgnored bool
}

type Preparation struct {
	Active            bool
	RuntimeCgroupPath string
	SanitizedResource *apipb.LinuxContainerResources
}

type RuntimePreparation struct {
	Request *apipb.CreateContainerRequest
	Options contract.HandlerOptions
}

func PrepareRuntime(request *apipb.CreateContainerRequest, options contract.HandlerOptions, policy RuntimePolicy) (RuntimePreparation, error) {
	result := RuntimePreparation{
		Request: request,
		Options: options,
	}
	if policy.IgnoreCgroups {
		if request.GetResource().GetMemoryLimitInBytes() > 0 {
			return RuntimePreparation{}, fmt.Errorf("memory limit requires cgroup enforcement; disabled_dev is not allowed")
		}
		result.Options.CgroupPath = ""
		result.Options.RuntimeCgroupPath = ""
		if policy.DropResourceWhenIgnored && request != nil && request.Resource != nil {
			result.Request = ocicli.CloneCreateRequestWithoutResource(request)
		}
		return result, nil
	}
	if strings.TrimSpace(options.CgroupPath) == "" {
		return RuntimePreparation{}, fmt.Errorf("cgroup enforcement is required but no sandbox cgroup was allocated")
	}

	prep, err := Prepare(request, options.CgroupPath)
	if err != nil {
		return RuntimePreparation{}, err
	}
	result.Options.RuntimeCgroupPath = prep.RuntimeCgroupPath
	result.Options.MemoryLimitBytes = request.GetResource().GetMemoryLimitInBytes()
	if result.Options.MemoryLimitBytes > 0 && !prep.Active {
		return RuntimePreparation{}, fmt.Errorf("memory limit requires an allocated cgroup path")
	}
	if request == nil || request.Resource == nil || !prep.Active {
		return result, nil
	}
	if result.Options.MemoryLimitBytes > 0 {
		prep.SanitizedResource = ocicli.CloneLinuxContainerResources(prep.SanitizedResource)
		prep.SanitizedResource.MemoryLimitInBytes = result.Options.MemoryLimitBytes
		prep.SanitizedResource.MemorySwapLimitInBytes = result.Options.MemoryLimitBytes
	}

	if prep.SanitizedResource != request.Resource {
		result.Request = ocicli.CloneCreateRequestWithoutResource(request)
		result.Request.Resource = prep.SanitizedResource
	}
	if err := updateCgroup(prep.RuntimeCgroupPath, prep.SanitizedResource); err != nil {
		return RuntimePreparation{}, fmt.Errorf("set cgroup resource limits on %s failed: %v", prep.RuntimeCgroupPath, err)
	}
	if result.Options.MemoryLimitBytes > 0 {
		if _, err := configureMemoryDomain(options.CgroupPath, prep.RuntimeCgroupPath, result.Options.MemoryLimitBytes); err != nil {
			return RuntimePreparation{}, fmt.Errorf("configure sandbox memory domain %s: %w", options.CgroupPath, err)
		}
	}
	return result, nil
}

func Prepare(request *apipb.CreateContainerRequest, cgroupPath string) (Preparation, error) {
	prep := Preparation{RuntimeCgroupPath: cgroupPath}
	if cgroupPath == "" {
		return prep, nil
	}

	driver, err := defaultCgroupDriver()
	if err != nil {
		return prep, fmt.Errorf("load cgroup driver failed: %v", err)
	}

	var resource *apipb.LinuxContainerResources
	if request != nil {
		resource = request.Resource
	}

	prep.Active = true
	prep.RuntimeCgroupPath = runtimeCgroupPath(driver, cgroupPath)
	prep.SanitizedResource = sanitizeResourceForDriver(driver, prep.RuntimeCgroupPath, resource)
	return prep, nil
}
