package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func NewRunscHandler(cfg config.Config) (contract.SandboxRuntime, error) {
	runtimeCfg := cfg.RuntimeConfig.Runsc
	if runtimeCfg.Binary == "" {
		return nil, fmt.Errorf("runsc binary is not configured")
	}
	if _, err := os.Stat(runtimeCfg.Binary); err != nil {
		return nil, err
	}
	handler, err := newRunscServiceHandler(cfg, runtimeCfg)
	if err != nil {
		return nil, err
	}
	if handler == nil {
		return nil, fmt.Errorf("runsc constructor returned a nil handler")
	}
	return handler, nil
}

func newRunscServiceHandler(cfg config.Config, runtimeCfg config.RuntimeInstanceConfig) (contract.SandboxRuntime, error) {
	containerRoot := filepath.Join(cfg.RootDir, "containers")
	loader, err := runtimeoci.NewBundleLoader(
		runtimeCfg.BaseSpec,
		containerRoot,
		runtimeoci.WithRuntimeDNSConfig(bundleflow.DNSConfigFromRuntimeConfig(cfg.RuntimeConfig.DNS)),
	)
	if err != nil {
		return nil, err
	}
	return NewRunscServiceHandler(cfg, runtimeCfg, loader)
}

func NewRunscServiceHandler(cfg config.Config, runtimeCfg config.RuntimeInstanceConfig, loader runtimeoci.Loader) (*RunscServiceHandler, error) {
	cgroupMode, err := cfg.RuntimeConfig.CgroupEnforcementMode()
	if err != nil {
		return nil, err
	}
	containerRoot := filepath.Join(cfg.RootDir, "containers")
	filestoreDir, releaseFilestore, err := acquireRuntimeFilestore(cfg)
	if err != nil {
		return nil, err
	}
	constructed := false
	defer func() {
		if !constructed {
			releaseFilestore(true)
		}
	}()
	rootfsViews := rootfsview.NewOverlayProvider(filestoreDir)
	writableCapacity, err := sharedWritableCapacityManager(filestoreDir, cfg.RuntimeConfig.FilestoreSystemReserveBytes)
	if err != nil {
		return nil, err
	}

	common, err := ocihost.New(ocihost.Config{
		Root:          cfg.RootDir,
		RuntimeBinary: runtimeCfg.Binary,
		Loader:        loader,
	})
	if err != nil {
		return nil, err
	}

	handler := &RunscServiceHandler{
		common:                            common,
		ignoreCgroups:                     cgroupMode == config.CgroupEnforcementDisabledDev,
		allowSUID:                         runtimeCfg.Options.AllowSUIDEnabled(true),
		filestoreDir:                      filestoreDir,
		ephemeralStorageDefaultLimitBytes: cfg.RuntimeConfig.EphemeralStorageDefaultLimitBytes,
		writableCapacity:                  writableCapacity,
		containerRoot:                     containerRoot,
		rootfsViews:                       rootfsViews,
		releaseFilestore:                  func() { releaseFilestore(false) },
		waitForSandboxReady:               runtimesandboxd.WaitReadyForContainer,
		newWorkloadClient: func(socketPath string) sandboxdWorkloadClient {
			return runtimesandboxd.NewClient(socketPath)
		},
	}
	handler.services = newRuntimeServices(containerRoot)
	constructed = true
	return handler, nil
}
