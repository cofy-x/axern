package handlerregistry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
)

func registryLoadConfig(t *testing.T, _ string) config.Config {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "runtime")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write runtime binary: %v", err)
	}
	return config.Config{
		RootDir:      root,
		PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{Runsc: config.RuntimeInstanceConfig{Binary: binary}}},
	}
}

func TestRegistryLoadRetriesUntilConfiguredRuntimeIsAvailable(t *testing.T) {
	runtimeName := config.RuntimeNameRunsc
	var attempts atomic.Int32
	runtimecore.RegisterRuntimeFactory(runtimeName, runtimecore.RuntimeFactoryFunc(func(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("runtime state is still busy")
		}
		return &runtimetest.FakeRuntimeHandler{RuntimeName: runtimeName}, nil
	}))

	registry := New(registryLoadConfig(t, runtimeName))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := registry.Load(ctx); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("factory attempts = %d, want 3", got)
	}
	if _, ok := registry.Get(runtimeName); !ok {
		t.Fatalf("runtime %q was not loaded", runtimeName)
	}
}

func TestRegistryLoadNeverReturnsPartialSuccess(t *testing.T) {
	runtimeName := config.RuntimeNameRunsc
	runtimecore.RegisterRuntimeFactory(runtimeName, runtimecore.RuntimeFactoryFunc(func(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		return nil, errors.New("runtime state is still busy")
	}))

	registry := New(registryLoadConfig(t, runtimeName))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := registry.Load(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load() error = %v, want context deadline exceeded", err)
	}
	if registry.Count() != 0 {
		t.Fatalf("registry count = %d, want 0", registry.Count())
	}
}

func TestRegistryLoadRequiresContext(t *testing.T) {
	err := New(config.Config{}).Load(nil)
	if err == nil || err.Error() != "runtime handler load context is required" {
		t.Fatalf("Load(nil) error = %v", err)
	}
}

func TestRegistryLoadDoesNotStartFactoriesAfterCancellation(t *testing.T) {
	runtimeName := config.RuntimeNameRunsc
	var attempts atomic.Int32
	runtimecore.RegisterRuntimeFactory(runtimeName, runtimecore.RuntimeFactoryFunc(func(config.Config, string, config.RuntimeInstanceConfig) (contract.RuntimeHandler, error) {
		attempts.Add(1)
		return &runtimetest.FakeRuntimeHandler{RuntimeName: runtimeName}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := New(registryLoadConfig(t, runtimeName)).Load(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v, want context canceled", err)
	}
	if got := attempts.Load(); got != 0 {
		t.Fatalf("factory attempts = %d, want 0", got)
	}
}

func TestRegistryStatuses(t *testing.T) {
	registry := New(config.Config{
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{Runsc: config.RuntimeInstanceConfig{Binary: "/fake/runsc"}},
		},
	})
	registry.Set("runsc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runsc"})

	statuses := registry.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("Statuses() len = %d, want 1", len(statuses))
	}
	if statuses[0].Name != "runsc" {
		t.Fatalf("Statuses() names = %+v", statuses)
	}
	if !statuses[0].Loaded {
		t.Fatalf("Statuses()[0].Loaded = false, want true")
	}
}

func TestRegistryNamesStableSorted(t *testing.T) {
	registry := New(config.Config{})
	registry.Set("other", &runtimetest.FakeRuntimeHandler{RuntimeName: "other"})
	registry.Set("runsc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runsc"})
	registry.Set("crun", &runtimetest.FakeRuntimeHandler{RuntimeName: "crun"})

	names := registry.Names()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Names() returned unsorted names: %v", names)
	}
	want := []string{"crun", "other", "runsc"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q (all=%v)", i, names[i], want[i], names)
		}
	}
}

func TestRegistryVersionFallsBackToUnknown(t *testing.T) {
	registry := New(config.Config{})
	registry.Set("runsc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runsc"})

	version, err := registry.Version(context.WithValue(context.Background(), "ERROR", "boom"))
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if version.GetVersion() != config.UnknownVersion {
		t.Fatalf("Version() runsc version = %q, want %q", version.GetVersion(), config.UnknownVersion)
	}
}
