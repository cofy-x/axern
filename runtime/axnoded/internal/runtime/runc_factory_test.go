package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/stretchr/testify/assert"
)

func TestRuncRuntimeFactoryRegistered(t *testing.T) {
	rootDir := t.TempDir()
	binDir := filepath.Join(rootDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	binPath := filepath.Join(binDir, "runc")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake runtime binary: %v", err)
	}

	cfg := config.Config{
		RootDir: rootDir,
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{
				Runtimes: map[string]config.RuntimeInstanceConfig{
					config.RuntimeNameRunc: {
						Binary: binPath,
					},
				},
			},
		},
	}

	handler, err := GetRuntimeHandler(cfg, config.RuntimeNameRunc)
	if err != nil {
		t.Fatalf("GetRuntimeHandler() error = %v", err)
	}
	if handler.Name() != config.RuntimeNameRunc {
		t.Fatalf("expected handler name %q, got %q", config.RuntimeNameRunc, handler.Name())
	}
	reqs := handler.Requirements()
	assert.Equal(t, []resourcemanager.ResourceName{
		resourcemanager.CgroupResourceName,
		resourcemanager.InterfaceResourceName,
	}, reqs.Resources)
	assert.True(t, reqs.NeedsCgroup)
	assert.True(t, reqs.NeedsNetworkNamespace)
}

func TestRuncFactoryComposesRuntimeServices(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}

	if handler.FileService() == nil {
		t.Fatal("expected runc file service to be composed")
	}
	if handler.ProcessService() == nil {
		t.Fatal("expected runc process service to be composed")
	}
}
