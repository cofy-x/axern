package servicekernel

import (
	"strings"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/proto"
)

func configEqual(a, b *commonv1.ExecutionConfig) bool {
	allocation := executionkernel.NormalizeConfig(a)
	desired := executionkernel.NormalizeConfig(b)

	// Writable-layer defaults are resolved only after the environment rootfs is
	// known, immediately before allocation admission. The service keeps the
	// user's unresolved intent, so compare it with the same resolved contract
	// whenever the admitted allocation has a writable layer. Without this, an
	// omitted request/limit makes every healthy writable-rootfs allocation look
	// outdated and starts a perpetual replacement rollout.
	allocationWritable := allocation.GetResources().GetRequests().GetWritableLayerBytes() > 0 ||
		allocation.GetResources().GetLimits().GetWritableLayerBytes() > 0
	if allocationWritable {
		resolved, err := executionkernel.NormalizeConfigForRootfs(desired, false)
		if err != nil {
			return false
		}
		desired = resolved
	}
	return proto.Equal(allocation, desired)
}

func probeEqual(a, b *servicev1.ServiceProbe) bool {
	return proto.Equal(cloneProbe(a), cloneProbe(b))
}

func allocationMatchesDesired(environmentID string, config *commonv1.ExecutionConfig, readinessProbe, livenessProbe *servicev1.ServiceProbe, service *servicev1.Service) bool {
	if service == nil {
		return false
	}
	return strings.TrimSpace(environmentID) == strings.TrimSpace(service.GetEnvironmentID()) &&
		configEqual(config, service.GetConfig()) &&
		probeEqual(readinessProbe, service.GetReadinessProbe()) &&
		probeEqual(livenessProbe, service.GetLivenessProbe())
}

func AllocationMatchesDesired(environmentID string, config *commonv1.ExecutionConfig, readinessProbe, livenessProbe *servicev1.ServiceProbe, service *servicev1.Service) bool {
	return allocationMatchesDesired(environmentID, config, readinessProbe, livenessProbe, service)
}

func allocationOutdated(environmentID string, config *commonv1.ExecutionConfig, readinessProbe, livenessProbe *servicev1.ServiceProbe, service *servicev1.Service) bool {
	return !allocationMatchesDesired(environmentID, config, readinessProbe, livenessProbe, service)
}

func AllocationOutdated(environmentID string, config *commonv1.ExecutionConfig, readinessProbe, livenessProbe *servicev1.ServiceProbe, service *servicev1.Service) bool {
	return allocationOutdated(environmentID, config, readinessProbe, livenessProbe, service)
}
