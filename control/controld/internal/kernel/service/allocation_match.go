package servicekernel

import (
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/proto"
)

func configEqual(a, b *commonv1.ExecutionConfig) bool {
	return proto.Equal(cloneConfig(a), cloneConfig(b))
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
