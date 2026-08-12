package service

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
	"github.com/sirupsen/logrus"
)

func (h *sandboxService) initContainerRuntime(ctx context.Context) (chan bool, error) {
	if err := h.runtimeHandlers.Load(ctx); err != nil {
		return nil, err
	}
	if err := validateRuntimeResourceConfiguration(h.runtimeHandlers, h.config.PluginConfig.ResourceConfig); err != nil {
		return nil, err
	}

	resourceManagers, err := resourcemanager.NewResourceManager(h.store, h.config)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("resource manager init success with config: %v", h.config.PluginConfig.ResourceConfig)

	if err = os.MkdirAll(h.config.RootDir, 0755); err != nil {
		shutdownResourceManagers(resourceManagers)
		return nil, err
	}

	healthChan := make(chan bool)
	h.containerManager, err = container.NewManager(
		h.config.RootDir,
		h.runtimeHandlers.Map(),
		healthChan,
		resourceManagers...,
	)
	if err != nil {
		shutdownResourceManagers(resourceManagers)
		return nil, err
	}
	h.containerManager.SetExitClassifier(h.classifyContainerExit)
	h.containerManager.SetExitObserver(h.handleContainerExitControlPlaneReport)
	return healthChan, nil
}

func shutdownResourceManagers(managers []resourcemanager.Manager) {
	for _, manager := range managers {
		if manager != nil {
			_ = manager.ShutDown()
		}
	}
}

func validateRuntimeResourceConfiguration(registry *handlerregistry.Registry, cfg config.ResourceConfig) error {
	if cfg.MaxInstanceNum <= 0 {
		return fmt.Errorf("invalid runtime resource configuration: max_instance_num must be positive")
	}
	if cfg.MaxInstanceNum > container.MaxContainerNum {
		return fmt.Errorf(
			"invalid runtime resource configuration: max_instance_num %d exceeds container hard limit %d",
			cfg.MaxInstanceNum,
			container.MaxContainerNum,
		)
	}
	configuredPools := []struct {
		name string
		size int
	}{
		{name: "cgroup_cache_size", size: cfg.CgroupCacheSize},
		{name: "interface_cache_size", size: cfg.InterfaceCacheSize},
	}
	for _, pool := range configuredPools {
		if pool.size < 0 {
			return fmt.Errorf("invalid runtime resource configuration: %s must not be negative", pool.name)
		}
		if pool.size > cfg.MaxInstanceNum {
			return fmt.Errorf(
				"invalid runtime resource configuration: %s %d exceeds max_instance_num %d",
				pool.name,
				pool.size,
				cfg.MaxInstanceNum,
			)
		}
	}

	disabled := disabledResourcePools(cfg, !runtimeRegistryRequiresResource(registry, resourcemanager.CgroupResourceName))
	disabledSet := make(map[resourcemanager.ResourceName]struct{}, len(disabled))
	for _, name := range disabled {
		disabledSet[name] = struct{}{}
	}
	var conflicts []string
	for runtimeName, handler := range registry.Items() {
		for _, resourceName := range handler.Requirements().Resources {
			if _, disabled := disabledSet[resourceName]; disabled {
				conflicts = append(conflicts, fmt.Sprintf("runtime %q requires disabled resource pool %q", runtimeName, resourceName))
			}
		}
	}
	if len(conflicts) == 0 {
		return nil
	}
	sort.Strings(conflicts)
	return fmt.Errorf("invalid runtime resource configuration: %s", strings.Join(conflicts, "; "))
}

func runtimeRegistryRequiresResource(registry *handlerregistry.Registry, want resourcemanager.ResourceName) bool {
	for _, handler := range registry.Items() {
		for _, resourceName := range handler.Requirements().Resources {
			if resourceName == want {
				return true
			}
		}
	}
	return false
}

func disabledResourcePools(cfg config.ResourceConfig, cgroupDisabled bool) []resourcemanager.ResourceName {
	disabled := make([]resourcemanager.ResourceName, 0, 2)
	if cgroupDisabled {
		disabled = append(disabled, resourcemanager.CgroupResourceName)
	}
	if cfg.InterfaceCacheSize <= 0 {
		disabled = append(disabled, resourcemanager.InterfaceResourceName)
	}
	return disabled
}

func (h *sandboxService) watchContainerReadiness(healthChan <-chan bool) {
	go func() {
		for ready := range healthChan {
			h.ready.Store(ready)
		}
	}()
}
