package oci

import (
	"maps"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// ResourcePolicy controls resource-related mutations on generated OCI specs.
type ResourcePolicy struct {
	IgnoreAnnotationKeys []string
}

// DefaultResourcePolicy returns the default resource policy.
func DefaultResourcePolicy() ResourcePolicy {
	return ResourcePolicy{
		IgnoreAnnotationKeys: []string{
			ignoreResourceFieldAnnoKey,
		},
	}
}

func setSpecResource(ociSpec *spec.Spec, resource *apipb.LinuxContainerResources) {
	if ociSpec == nil || ociSpec.Linux == nil {
		return
	}
	if resource == nil {
		ociSpec.Linux.Resources = nil
		return
	}
	if ociSpec.Linux.Resources == nil {
		ociSpec.Linux.Resources = &spec.LinuxResources{}
	}
	if ociSpec.Linux.Resources.CPU == nil {
		ociSpec.Linux.Resources.CPU = &spec.LinuxCPU{}
	}
	if ociSpec.Linux.Resources.Memory == nil {
		ociSpec.Linux.Resources.Memory = &spec.LinuxMemory{}
	}
	if ociSpec.Linux.Resources.HugepageLimits == nil {
		ociSpec.Linux.Resources.HugepageLimits = []spec.LinuxHugepageLimit{}
	}
	if ociSpec.Linux.Resources.Unified == nil {
		ociSpec.Linux.Resources.Unified = map[string]string{}
	}

	if resource.CpuShares > 0 {
		ociSpec.Linux.Resources.CPU.Shares = &resource.CpuShares
	}
	if resource.CpuQuota > 0 {
		ociSpec.Linux.Resources.CPU.Quota = &resource.CpuQuota
	}
	if resource.CpuPeriod > 0 {
		ociSpec.Linux.Resources.CPU.Period = &resource.CpuPeriod
	}
	if resource.CpusetCpus != "" {
		ociSpec.Linux.Resources.CPU.Cpus = resource.CpusetCpus
	}
	if resource.CpusetMems != "" {
		ociSpec.Linux.Resources.CPU.Mems = resource.CpusetMems
	}
	if resource.MemorySwapLimitInBytes > 0 {
		ociSpec.Linux.Resources.Memory.Swap = &resource.MemorySwapLimitInBytes
	}
	if resource.MemoryLimitInBytes > 0 {
		ociSpec.Linux.Resources.Memory.Limit = &resource.MemoryLimitInBytes
	}
	if resource.HugepageLimits != nil {
		for _, limit := range resource.HugepageLimits {
			ociSpec.Linux.Resources.HugepageLimits = append(ociSpec.Linux.Resources.HugepageLimits, spec.LinuxHugepageLimit{
				Pagesize: limit.PageSize,
				Limit:    limit.Limit,
			})
		}
	}
	if resource.Unified != nil {
		maps.Copy(ociSpec.Linux.Resources.Unified, resource.Unified)
	}
}

func (p ResourcePolicy) apply(ociSpec *spec.Spec) {
	if ociSpec == nil || ociSpec.Linux == nil {
		return
	}
	if !hasAnyAnnotation(ociSpec.Annotations, p.IgnoreAnnotationKeys...) {
		return
	}
	logrus.Debug("ignore resource field for spec materialization")
	ociSpec.Linux.Resources = nil
}
