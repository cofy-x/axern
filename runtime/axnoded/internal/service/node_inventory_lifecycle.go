package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	"github.com/sirupsen/logrus"
)

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
	disabledPools := disabledResourcePools(h.config.PluginConfig.ResourceConfig)
	h.nodeInventorySource = nodeinventory.NewAxnodedSource(nodeinventory.AxnodedSourceOptions{
		Ready:                 h.Ready,
		RuntimeCount:          h.runtimeHandlers.Count,
		Container:             h.containerManager,
		LangRuntime:           h.lrtManager,
		ImageManager:          nodeinventory.NewImageManagerClient(imageManagerEnabled, imageManagerSocket),
		NodeResources:         nodeResources,
		CgroupDriver:          inventoryCgroupDriver,
		NatBackend:            h.config.PluginConfig.NetworkConfig.NatBackend,
		BPFNetPinPath:         h.config.PluginConfig.NetworkConfig.BPFNet.PinPath,
		NodeState:             h.config.PluginConfig.ControlPlaneNodeStateValue(),
		NodeLabels:            h.config.PluginConfig.ControlPlaneNodeLabelsValue(),
		NodeCapabilities:      servicecontrolplane.DefaultNodeCapabilities(h.config),
		VolumeHealth:          h.volumeClient.Health,
		StorageTargets:        nodeinventory.DefaultStorageTargets(h.config.RootDir),
		RuntimeSlotCapacity:   h.config.PluginConfig.ResourceConfig.MaxInstanceNum,
		DisabledResourcePools: disabledPools,
	})
	h.inventoryCollector = nodeinventory.NewCollector(5*time.Second, h.nodeInventorySource.Collect)
	return nil
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
