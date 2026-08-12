package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/sirupsen/logrus"
)

const capabilityRefreshInterval = 5 * time.Second

func (h *sandboxService) initNodeInventory() error {
	imageManagerEnabled := h.config.PluginConfig.RuntimeConfig.ImageManagerEnabledValue()
	imageManagerSocket := h.config.PluginConfig.RuntimeConfig.ImageManagerSocketPath()
	nodeResources, err := nodeResourceProvider(h.config.PluginConfig)
	if err != nil {
		return err
	}
	var inventoryCgroupDriver os2.CgroupDriver
	if inventoryCgroupDriver, err = os2.DefaultCgroupDriver(); err != nil {
		logrus.WithError(err).Warn("node inventory actual usage provider disabled: cgroup driver unavailable")
	}
	cgroupMode, err := h.config.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return err
	}
	rootName, err := h.config.PluginConfig.ResourceConfig.CgroupRootNameValue()
	if err != nil {
		return err
	}
	if cgroupMode == config.CgroupEnforcementRequired {
		if inventoryCgroupDriver == nil {
			return fmt.Errorf("resolve delegated cgroup root: cgroup driver is unavailable")
		}
		rootName, err = inventoryCgroupDriver.ResolveRoot(rootName)
		if err != nil {
			return fmt.Errorf("resolve delegated sandbox cgroup root: %w", err)
		}
		if err := h.initializeMemoryObservationSequence(); err != nil {
			return err
		}
		logrus.Debug("cgroup memory capability will be evaluated by observed-capability provider")
	}
	h.capabilityManager, err = h.newObservedCapabilityManager(rootName)
	if err != nil {
		return err
	}
	h.capabilityManager.Subscribe(h.handleCapabilityTransitions)
	h.capabilityManager.SetMetricsObserver(&capabilityMetricsObserver{})
	disabledPools := disabledResourcePools(
		h.config.PluginConfig.ResourceConfig,
		cgroupMode != config.CgroupEnforcementRequired,
	)
	storageTargets := nodeinventory.DefaultStorageTargets(h.config.RootDir)
	if filestore := h.config.PluginConfig.RuntimeConfig.FilestoreDir; filestore != "" {
		storageTargets = append(storageTargets, nodeinventory.StorageTarget{
			Target: nodeinventory.StorageTargetRuntimeFilestore, Path: filestore,
			SystemReserveBytes: h.config.PluginConfig.RuntimeConfig.FilestoreSystemReserveBytes,
		})
	}
	var nextMemoryObservationRevision func() (int64, error)
	memoryCommitment := h.containerManager.MemoryCommitment
	memoryCapacityObserver := h.containerManager.UpdateMemoryCapacity
	if cgroupMode == config.CgroupEnforcementRequired {
		nextMemoryObservationRevision = h.nextMemoryObservationRevision
	} else {
		// disabled_dev has no cgroup resource manager by construction. It still
		// publishes resource-source scheduling capacity, but must not pretend to
		// own kernel-backed commitments or a node-local hard-admission gate.
		memoryCommitment = func() (resources.MemoryCommitment, error) {
			return resources.MemoryCommitment{}, nil
		}
		memoryCapacityObserver = func(resources.MemoryCapacitySnapshot) error { return nil }
	}
	hostname, _ := os.Hostname()
	h.nodeInventorySource = nodeinventory.NewAxnodedSource(nodeinventory.AxnodedSourceOptions{
		NodeID:                    h.config.PluginConfig.ControlPlaneNodeIDValue(hostname),
		Ready:                     h.Ready,
		RuntimeCount:              h.runtimeHandlers.Count,
		Container:                 h.containerManager,
		LangRuntime:               h.lrtManager,
		ImageManager:              nodeinventory.NewImageManagerClient(imageManagerEnabled, imageManagerSocket),
		NodeResources:             nodeResources,
		CgroupDriver:              inventoryCgroupDriver,
		NatBackend:                h.config.PluginConfig.NetworkConfig.NatBackend,
		BPFNetPinPath:             h.config.PluginConfig.NetworkConfig.BPFNet.PinPath,
		NodeState:                 h.config.PluginConfig.ControlPlaneNodeStateValue(),
		NodeLabels:                h.config.PluginConfig.ControlPlaneNodeLabelsValue(),
		CapabilitySnapshot:        h.currentCapabilitySnapshot,
		VolumeHealth:              h.volumeClient.Health,
		StorageTargets:            storageTargets,
		RuntimeSlotCapacity:       h.config.PluginConfig.ResourceConfig.MaxInstanceNum,
		MemoryBudgetEnabled:       true,
		MemoryCgroupEnforced:      cgroupMode == config.CgroupEnforcementRequired,
		CgroupRootName:            rootName,
		MemorySystemReserveBytes:  h.config.PluginConfig.ResourceConfig.MemorySystemReserveBytes,
		MemoryCommitment:          memoryCommitment,
		MemoryCapacityObserver:    memoryCapacityObserver,
		MemoryObservationRevision: nextMemoryObservationRevision,
		MemoryPIDRolesVerifier:    h.verifyMemoryPIDRoles,
		RetiringMemoryLeases:      h.containerManager.RetiringMemoryLeases,
		UnackedStatusIDs:          h.controlPlaneReports.UnacknowledgedAllocationStatusIDs,
		DisabledResourcePools:     disabledPools,
	})
	h.inventoryCollector = nodeinventory.NewCollector(5*time.Second, h.nodeInventorySource.Collect)
	return nil
}

func (h *sandboxService) currentCapabilitySnapshot(context.Context, time.Time) (*capabilityv1.CapabilitySnapshot, error) {
	if h == nil || h.capabilityManager == nil {
		return nil, fmt.Errorf("capability manager is unavailable")
	}
	snapshot := h.capabilityManager.Snapshot()
	if snapshot == nil || !h.capabilityManager.Ready() {
		return nil, nodeinventory.ErrCapabilitySnapshotWarming
	}
	return snapshot, nil
}

// startCapabilityRefresh starts independent provider schedulers and a separate
// inventory publisher. Inventory consumes the latest atomic publication; it
// never forms a global barrier around runtime conformance and health probes.
func (h *sandboxService) startCapabilityRefresh(parent context.Context) {
	if h == nil || h.capabilityManager == nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	h.capabilityRefreshCancel = cancel
	h.capabilityManager.Start(ctx)
	h.capabilityRefreshWG.Add(1)
	go func() {
		defer h.capabilityRefreshWG.Done()
		ticker := time.NewTicker(capabilityRefreshInterval)
		defer ticker.Stop()
		for {
			h.refreshNodeInventory()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (h *sandboxService) stopCapabilityRefresh() {
	if h == nil {
		return
	}
	if h.capabilityRefreshCancel != nil {
		h.capabilityRefreshCancel()
		h.capabilityRefreshCancel = nil
	}
	h.capabilityRefreshWG.Wait()
}

func (h *sandboxService) refreshNodeInventory() {
	if h == nil || h.inventoryCollector == nil {
		return
	}
	h.inventoryCollector.Refresh(context.Background())
}

func (h *sandboxService) NodeInventory() (nodeinventory.NodeInventorySnapshot, bool) {
	if h.inventoryCollector == nil {
		return nodeinventory.NewSnapshot(), false
	}
	return h.inventoryCollector.Snapshot()
}

func nodeResourceProvider(cfg config.PluginConfig) (nodeinventory.NodeResourceProvider, error) {
	source, err := cfg.ControlPlaneNodeResourceSourceValue()
	if err != nil {
		return nil, err
	}
	switch source {
	case config.ControlPlaneNodeResourceSourceKubernetes:
		provider, err := nodeinventory.NewKubernetesNodeResourceProvider(nodeinventory.KubernetesNodeResourceProviderOptions{
			NodeName: cfg.ControlPlaneKubernetesNodeNameValue(cfg.ControlPlaneNodeIDValue("")),
		})
		if err != nil {
			logrus.WithError(err).Warn("kubernetes node resource provider unavailable")
			return nodeinventory.NewErrorNodeResourceProvider(fmt.Errorf("kubernetes node resource provider unavailable: %w", err)), nil
		}
		return provider, nil
	default:
		return nodeinventory.NewHostNodeResourceProvider(), nil
	}
}
