package service

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func (h *sandboxService) configureAllocationController() {
	h.allocations = allocation.NewController(h.allocationOptions())
}

func (h *sandboxService) allocationOptions() allocation.Options {
	return allocation.Options{
		Config: h.config,
		Store:  h.store,
		ContainerManager: func() *container.Manager {
			return h.containerManager
		},
		RuntimeHandler:              h.runtimeHandler,
		LangRuntime:                 h.lrtManager,
		Volumes:                     h.volumes,
		Networking:                  h.networking,
		Probes:                      h.probeCoordinator,
		ReportStatus:                h.ReportAllocationStatus,
		InventoryChanged:            h.notifyNodeInventoryChanged,
		RootfsCapabilityGate:        h.verifyRootfsCapabilityRequirements,
		PreActivationCapabilityGate: h.verifyPreparedAllocationCapabilities,
		Egress:                      h.egressClient,
	}
}

func (h *sandboxService) allocationController() *allocation.Controller {
	if h == nil {
		return nil
	}
	if h.allocations != nil {
		return h.allocations
	}
	h.allocations = allocation.NewController(h.allocationOptions())
	return h.allocations
}

func (h *sandboxService) WorkspacePreparation(containerID string) *commonv1.WorkspacePreparationFacts {
	return h.allocationController().WorkspacePreparation(containerID)
}
