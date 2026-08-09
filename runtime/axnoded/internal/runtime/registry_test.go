package runtime_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
)

func runtimeRegistryTestConfig(t *testing.T, runtimeName string) config.Config {
	t.Helper()
	rootDir := t.TempDir()
	binPath := filepath.Join(rootDir, "fake-runtime")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake runtime binary: %v", err)
	}
	return config.Config{
		RootDir: rootDir,
		PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{
			Runtimes: map[string]config.RuntimeInstanceConfig{
				runtimeName: {Binary: binPath},
			},
		}},
	}
}

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

func TestGetRuntimeHandlerRejectsNilFactoryResult(t *testing.T) {
	runtimeName := fmt.Sprintf("nil-runtime-%d", time.Now().UnixNano())
	runtimecore.RegisterRuntimeFactory(runtimeName, runtimecore.RuntimeFactoryFunc(func(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		return nil, nil
	}))
	cfg := runtimeRegistryTestConfig(t, runtimeName)

	handler, err := runtimecore.GetRuntimeHandler(cfg, runtimeName)
	if err == nil || !strings.Contains(err.Error(), "factory returned a nil handler") {
		t.Fatalf("GetRuntimeHandler() error = %v", err)
	}
	if handler != nil {
		t.Fatalf("GetRuntimeHandler() handler = %T, want nil", handler)
	}
}

func TestGetRuntimeHandlerRejectsMismatchedHandlerName(t *testing.T) {
	runtimeName := fmt.Sprintf("mismatched-runtime-%d", time.Now().UnixNano())
	runtimecore.RegisterRuntimeFactory(runtimeName, runtimecore.RuntimeFactoryFunc(func(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		return &runtimetest.FakeRuntimeHandler{RuntimeName: "different-runtime"}, nil
	}))
	cfg := runtimeRegistryTestConfig(t, runtimeName)

	handler, err := runtimecore.GetRuntimeHandler(cfg, runtimeName)
	if err == nil || !strings.Contains(err.Error(), "factory returned handler named different-runtime") {
		t.Fatalf("GetRuntimeHandler() error = %v", err)
	}
	if handler != nil {
		t.Fatalf("GetRuntimeHandler() handler = %T, want nil", handler)
	}
}
