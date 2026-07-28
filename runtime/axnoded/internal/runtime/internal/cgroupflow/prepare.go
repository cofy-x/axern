package cgroupflow

import (
	"fmt"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
)

var (
	defaultCgroupDriver        = os2.DefaultCgroupDriver
	runtimeCgroupPath          = hostlinux.RuntimeCgroupPath
	sanitizeResourceForDriver  = hostlinux.SanitizeResourceForDriver
	updateCgroup               = hostlinux.UpdateCgroup
	isCgroupWritePermissionErr = hostlinux.IsCgroupWritePermissionError
)

type RuntimePolicy struct {
	IgnoreCgroups                bool
	DropResourceWhenIgnored      bool
	AllowWritePermissionFallback bool
}

type Preparation struct {
	Active            bool
	RuntimeCgroupPath string
	SanitizedResource *apipb.LinuxContainerResources
}

type RuntimePreparation struct {
	Request                 *apipb.CreateContainerRequest
	Options                 contract.HandlerOptions
	WritePermissionFallback bool
	WritePermissionError    error
}

func PrepareRuntime(request *apipb.CreateContainerRequest, options contract.HandlerOptions, policy RuntimePolicy) (RuntimePreparation, error) {
	result := RuntimePreparation{
		Request: request,
		Options: options,
	}
	if policy.IgnoreCgroups {
		result.Options.CgroupPath = ""
		result.Options.RuntimeCgroupPath = ""
		if policy.DropResourceWhenIgnored && request != nil && request.Resource != nil {
			result.Request = ocicli.CloneCreateRequestWithoutResource(request)
		}
		return result, nil
	}

	prep, err := Prepare(request, options.CgroupPath)
	if err != nil {
		return RuntimePreparation{}, err
	}
	result.Options.RuntimeCgroupPath = prep.RuntimeCgroupPath
	if request == nil || request.Resource == nil || !prep.Active {
		return result, nil
	}

	if prep.SanitizedResource != request.Resource {
		result.Request = ocicli.CloneCreateRequestWithoutResource(request)
		result.Request.Resource = prep.SanitizedResource
	}
	if err := updateCgroup(prep.RuntimeCgroupPath, prep.SanitizedResource); err != nil {
		if policy.AllowWritePermissionFallback && isCgroupWritePermissionErr(err) {
			result.WritePermissionFallback = true
			result.WritePermissionError = err
			result.Request = ocicli.CloneCreateRequestWithoutResource(request)
			return result, nil
		}
		return RuntimePreparation{}, fmt.Errorf("set cgroup resource limits on %s failed: %v", prep.RuntimeCgroupPath, err)
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
