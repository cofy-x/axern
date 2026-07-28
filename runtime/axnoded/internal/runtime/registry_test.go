package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
)

func TestGetRuntimeHandlerUsesRegisteredFactory(t *testing.T) {
	const runtimeName = "test-runtime-factory"

	runtimecore.RegisterRuntimeFactory(runtimeName, runtimecore.RuntimeFactoryFunc(func(cfg config.Config, configuredName string, runtimeCfg config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		return &runtimetest.FakeRuntimeHandler{
			RuntimeName: configuredName,
			RuntimeCapabilities: contract.RuntimeCapabilities{
				CanCheckpoint: true,
			},
			RuntimeRequirements: contract.RuntimeRequirements{
				Resources: []resourcemanager.ResourceName{resourcemanager.CgroupResourceName},
			},
		}, nil
	}))

	rootDir := t.TempDir()
	binPath := filepath.Join(rootDir, "fake-runtime")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake runtime binary: %v", err)
	}

	cfg := config.Config{
		RootDir: rootDir,
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{
				Runtimes: map[string]config.RuntimeInstanceConfig{
					runtimeName: {
						Binary: binPath,
					},
				},
			},
		},
	}

	handler, err := runtimecore.GetRuntimeHandler(cfg, runtimeName)
	if err != nil {
		t.Fatalf("GetRuntimeHandler() error = %v", err)
	}
	if handler.Name() != runtimeName {
		t.Fatalf("expected handler name %q, got %q", runtimeName, handler.Name())
	}
	if !handler.Capabilities().CanCheckpoint {
		t.Fatalf("expected registered factory capabilities to be preserved")
	}
	if len(handler.Requirements().Resources) != 1 || handler.Requirements().Resources[0] != resourcemanager.CgroupResourceName {
		t.Fatalf("unexpected runtime requirements: %+v", handler.Requirements())
	}

	version, err := handler.Version(context.Background())
	if err != nil {
		t.Fatalf("handler.Version() error = %v", err)
	}
	if version.GetRuntimeName() != runtimeName {
		t.Fatalf("expected runtime version to report %q, got %q", runtimeName, version.GetRuntimeName())
	}
}
