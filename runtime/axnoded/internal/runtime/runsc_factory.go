package runtime

import (
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/bundleflow"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func init() {
	RegisterRuntimeFactory(config.RuntimeNameRunsc, RuntimeFactoryFunc(func(cfg config.Config, runtimeName string, runtimeCfg config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		containerRoot := filepath.Join(cfg.RootDir, "containers")
		loader, err := runtimeoci.NewBundleLoader(
			runtimeCfg.BaseSpec,
			containerRoot,
			runtimeoci.WithRuntimeDNSConfig(bundleflow.DNSConfigFromRuntimeConfig(cfg.RuntimeConfig.DNS)),
		)
		if err != nil {
			return nil, err
		}
		return NewRunscServiceHandler(cfg, runtimeName, runtimeCfg, loader)
	}))
}

func NewRunscServiceHandler(cfg config.Config, runtimeName string, runtimeCfg config.RuntimeInstanceConfig, loader runtimeoci.Loader) (*RunscServiceHandler, error) {
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
		Root:                cfg.RootDir,
		RuntimeName:         runtimeName,
		RuntimeBinary:       runtimeCfg.Binary,
		RuntimeRunnerBinary: cfg.RuntimeConfig.RuntimeRunnerBinaryPath(),
		Loader:              loader,
	})
	if err != nil {
		return nil, err
	}

	handler := &RunscServiceHandler{
		name:                              runtimeName,
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
	}
	handler.services = newRuntimeServices(containerRoot, handler.OpenExecSession)
	constructed = true
	return handler, nil
}
