package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

func (h *sandboxService) initContainerRuntime(ctx context.Context) (chan bool, error) {
	handler, err := loadRunscHandler(ctx, h.config)
	if err != nil {
		return nil, err
	}
	h.runscHandler = handler
	if err := validateRuntimeResourceConfiguration(handler, h.config.PluginConfig.ResourceConfig); err != nil {
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
		h.runscHandler,
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

func loadRunscHandler(ctx context.Context, cfg config.Config) (contract.RuntimeHandler, error) {
	backoff := 100 * time.Millisecond
	for {
		handler, err := runtimecore.NewRunscHandler(cfg)
		if err == nil {
			logrus.Info("loaded runsc handler")
			return handler, nil
		}
		logrus.WithError(err).Warn("load runsc handler")
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, fmt.Errorf("load runsc handler: %w", ctx.Err())
		case <-timer.C:
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

func shutdownResourceManagers(managers []resourcemanager.Manager) {
	for _, manager := range managers {
		if manager != nil {
			_ = manager.ShutDown()
		}
	}
}

func validateRuntimeResourceConfiguration(handler contract.RuntimeHandler, cfg config.ResourceConfig) error {
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

	disabled := disabledResourcePools(cfg, !runtimeRequiresResource(handler, resourcemanager.CgroupResourceName))
	disabledSet := make(map[resourcemanager.ResourceName]struct{}, len(disabled))
	for _, name := range disabled {
		disabledSet[name] = struct{}{}
	}
	for _, resourceName := range handler.Requirements().Resources {
		if _, disabled := disabledSet[resourceName]; disabled {
			return fmt.Errorf("invalid runtime resource configuration: runsc requires disabled resource pool %q", resourceName)
		}
	}
	return nil
}

func runtimeRequiresResource(handler contract.RuntimeHandler, want resourcemanager.ResourceName) bool {
	for _, resourceName := range handler.Requirements().Resources {
		if resourceName == want {
			return true
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
