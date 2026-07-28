package runtime

import (
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/envelopepolicy"
)

func (r *RuncServiceHandler) Capabilities() contract.RuntimeCapabilities {
	return contract.RuntimeCapabilities{CanExecDirect: true}
}

func (r *RuncServiceHandler) ProcessService() contract.ProcessService {
	return r.services.process
}

func (r *RuncServiceHandler) EligibleForExecutionEnvelope(request *runtimeapi.StartRequest) bool {
	return envelopepolicy.EligibleForStaticEnvelope(request, config.RuntimeNameRunc)
}

func (r *RuncServiceHandler) Requirements() contract.RuntimeRequirements {
	resources := []resourcemanager.ResourceName{
		resourcemanager.InterfaceResourceName,
	}
	if !r.ignoreCgroups {
		resources = append([]resourcemanager.ResourceName{resourcemanager.CgroupResourceName}, resources...)
	}
	return contract.RuntimeRequirements{
		NeedsCgroup:           !r.ignoreCgroups,
		NeedsNetworkNamespace: true,
		Resources:             resources,
	}
}
