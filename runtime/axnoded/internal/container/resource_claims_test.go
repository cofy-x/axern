package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	cmap "github.com/orcaman/concurrent-map/v2"
)

func TestRuntimeCgroupPathUsesRuntimeProjection(t *testing.T) {
	root := t.TempDir()
	manager := &Manager{root: root, recyclePath: t.TempDir(), containers: cmap.New[*Container]()}
	containerDir := filepath.Join(root, "allocation-1")
	if err := os.MkdirAll(containerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), []byte(`{"ociVersion":"1.0.0","linux":{"cgroupsPath":"/sandbox/test/workload"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := manager.RuntimeCgroupPath("allocation-1")
	if err != nil || got != "/sandbox/test/workload" {
		t.Fatalf("RuntimeCgroupPath() = %q, %v", got, err)
	}
}

func TestAllocationCgroupPathUsesTypedLease(t *testing.T) {
	owner := &releaseTrackingResourceManager{
		name: resourcemanager.CgroupResourceName,
		owners: map[string]string{"allocation-1": "/sandbox/test"},
	}
	manager := &Manager{containers: cmap.New[*Container](), resourceManagers: cmap.New[resourcemanager.Manager]()}
	manager.resourceManagers.Set(string(resourcemanager.CgroupResourceName), owner)
	got, err := manager.AllocationCgroupPath("allocation-1")
	if err != nil || got != "/sandbox/test" {
		t.Fatalf("AllocationCgroupPath() = %q, %v", got, err)
	}
}

func TestParseProcessCgroupPathPrefersUnifiedPath(t *testing.T) {
	got, err := parseProcessCgroupPath("0::/sandbox/test/workload\n")
	if err != nil || got != "/sandbox/test/workload" {
		t.Fatalf("parseProcessCgroupPath() = %q, %v", got, err)
	}
}

func TestParseProcessCgroupPathFallsBackToMemoryController(t *testing.T) {
	got, err := parseProcessCgroupPath("11:cpu:/sandbox/test\n10:memory:/sandbox/test/workload\n")
	if err != nil || got != "/sandbox/test/workload" {
		t.Fatalf("parseProcessCgroupPath() = %q, %v", got, err)
	}
}
