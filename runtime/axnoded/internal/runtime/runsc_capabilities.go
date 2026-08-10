package runtime

import (
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func (r *RunscServiceHandler) Capabilities() contract.RuntimeCapabilities {
	return contract.RuntimeCapabilities{
		CanCheckpoint: true,
		CanExecDirect: true,
	}
}

func (r *RunscServiceHandler) ProcessService() contract.ProcessService {
	return r.services.process
}

func (r *RunscServiceHandler) Requirements() contract.RuntimeRequirements {
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
