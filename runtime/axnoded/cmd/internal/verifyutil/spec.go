package verifyutil

import commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"

func BuildSandboxResources(requestCPUMilli, requestMemoryMiB, limitCPUMilli, limitMemoryMiB float64) *commonv1.ResourceSpec {
	resources := &commonv1.ResourceSpec{}
	requests := &commonv1.ResourceQuantity{}
	if requestCPUMilli > 0 {
		requests.CpuMilli = int64(requestCPUMilli)
	}
	if requestMemoryMiB > 0 {
		requests.MemoryBytes = int64(requestMemoryMiB * 1024 * 1024)
	}
	if requests.CpuMilli > 0 || requests.MemoryBytes > 0 {
		resources.Requests = requests
	}
	limits := &commonv1.ResourceQuantity{}
	if limitCPUMilli > 0 {
		limits.CpuMilli = int64(limitCPUMilli)
	}
	if limitMemoryMiB > 0 {
		limits.MemoryBytes = int64(limitMemoryMiB * 1024 * 1024)
	}
	if limits.CpuMilli > 0 || limits.MemoryBytes > 0 {
		resources.Limits = limits
	}
	if resources.Requests == nil && resources.Limits == nil {
		return nil
	}
	return resources
}
