package service

import (
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/rootfssnapshot"
)

func (h *sandboxService) configureAllocationController() {
	if h == nil || h.allocations != nil {
		return
	}
	h.allocations = allocation.NewController(h.allocationOptions())
}

func (h *sandboxService) allocationOptions() allocation.Options {
	var snapshotPublisher rootfssnapshot.Publisher
	if strings.TrimSpace(h.config.PluginConfig.RuntimeConfig.RootfsSnapshotRepository) != "" {
		snapshotPublisher = rootfssnapshot.NewPublisher(h.config.PluginConfig.RuntimeConfig.ImageManagerSocketPath())
	}
	return allocation.Options{
		Config: h.config,
		Store:  h.store,
		ContainerManager: func() *container.Manager {
			return h.containerManager
		},
		RunscHandler:                h.runscHandler,
		EnvironmentCache:            h.environmentCache,
		Networking:                  h.networking,
		ReportStatus:                h.ReportAllocationLifecycle,
		InventoryChanged:            h.notifyNodeInventoryChanged,
		RootfsCapabilityGate:        h.verifyRootfsCapabilityRequirements,
		PreActivationCapabilityGate: h.verifyPreparedAllocationCapabilities,
		Egress:                      h.egressClient,
		RootfsSnapshots:             snapshotPublisher,
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
