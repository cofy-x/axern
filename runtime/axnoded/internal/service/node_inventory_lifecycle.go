package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	os2 "github.com/cofy-x/axern/runtime/axnoded/internal/cgroup"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
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
	cgroupMemoryReady := false
	cgroupMode, err := h.config.PluginConfig.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return err
	}
	if cgroupMode == config.CgroupEnforcementRequired {
		rootName := h.config.PluginConfig.ResourceConfig.CgroupRootName
		if rootName == "" {
			rootName = config.DefaultCgroupRoot
		}
		if probeErr := hostlinux.ProbeCgroupMemoryLimit(rootName); probeErr != nil {
			logrus.WithError(probeErr).Warn("cgroup memory-limit admission capability unavailable")
		} else {
			cgroupMemoryReady = true
		}
	}
	disabledPools := disabledResourcePools(h.config.PluginConfig.ResourceConfig)
	storageTargets := nodeinventory.DefaultStorageTargets(h.config.RootDir)
	if filestore := h.config.PluginConfig.RuntimeConfig.FilestoreDir; filestore != "" {
		storageTargets = append(storageTargets, nodeinventory.StorageTarget{
			Target: nodeinventory.StorageTargetRuntimeFilestore, Path: filestore,
			SystemReserveBytes: h.config.PluginConfig.RuntimeConfig.FilestoreSystemReserveBytes,
		})
	}
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
		DynamicCapabilities:   func() []string { return runtimeStorageCapabilities(h.config, cgroupMemoryReady) },
		VolumeHealth:          h.volumeClient.Health,
		StorageTargets:        storageTargets,
		RuntimeSlotCapacity:   h.config.PluginConfig.ResourceConfig.MaxInstanceNum,
		DisabledResourcePools: disabledPools,
	})
	h.inventoryCollector = nodeinventory.NewCollector(5*time.Second, h.nodeInventorySource.Collect)
	return nil
}

func runtimeStorageCapabilities(cfg config.Config, cgroupMemoryReady bool) []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	if cgroupMemoryReady {
		add("cgroup:memory-limit-ready")
	}
	if entries, err := os.ReadDir(filepath.Join(cfg.RootDir, "verified-capabilities")); err == nil {
		bootID, _ := hostlinux.CurrentBootID()
		for _, entry := range entries {
			data, err := os.ReadFile(filepath.Join(cfg.RootDir, "verified-capabilities", entry.Name()))
			if err != nil || bootID == "" || string(data) != bootID+"\n" {
				continue
			}
			switch entry.Name() {
			case "runtime-runc-memory-hard-limit":
				add("runtime:runc:memory-hard-limit")
			case "runtime-runsc-memory-hard-limit":
				add("runtime:runsc:memory-hard-limit")
			}
		}
	}
	if filestore := cfg.PluginConfig.RuntimeConfig.FilestoreDir; filestore != "" {
		if facts, err := hostlinux.ReadFilestoreCapabilities(filestore); err == nil {
			if facts.OverlayReady {
				add("runtime:runsc:ephemeral-storage-hard-limit")
			}
			if facts.ProjectQuotaReady {
				add("runtime:runc:ephemeral-storage-hard-limit")
			}
			if facts.EROFSReady {
				add("rootfs-lower:erofs")
			}
		}
	}
	out := make([]string, 0, len(seen))
	for capability := range seen {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
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
