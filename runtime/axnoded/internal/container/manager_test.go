/**
 * Alipay.com Inc.
 * Copyright (c) 2004-2024 All Rights Reserved.
 */

package container

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/truncindex"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stopTestResourceManager struct {
	shutdowns int
}

func (m *stopTestResourceManager) Allocate(resourcemanager.AllocateOption) (resourcemanager.Resource, error) {
	return resourcemanager.EmptyStringResource, nil
}

func (m *stopTestResourceManager) Recycle(string) error { return nil }

func (m *stopTestResourceManager) Status() ([]string, []string) { return nil, nil }

func (m *stopTestResourceManager) ShutDown() error {
	m.shutdowns++
	return nil
}

func (m *stopTestResourceManager) ResourceName() resourcemanager.ResourceName {
	return resourcemanager.ResourceName("test")
}

func TestNewManager(t *testing.T) {
	handlers := cmap.New[contract.RuntimeHandler]()
	healthChan := make(chan bool)
	manager, err := resourcemanager.NewResourceManager(storetest.NewMockStore(), config.Config{
		PluginConfig: config.PluginConfig{
			ResourceConfig: config.ResourceConfig{
				MaxInstanceNum:     10,
				CgroupRootName:     "huse",
				CgroupCacheSize:    8,
				InterfaceCacheSize: 0,
				ResourceAdvanceConfig: config.ResourceAdvanceConfig{
					RecyclePolicy: config.RecyclePolicyDestroy,
				},
			},
		},
	})
	if err != nil && runtime.GOOS != "linux" {
		t.Skipf("resource manager is not host-safe on %s: %v", runtime.GOOS, err)
	}
	assert.NotNil(t, manager)
	assert.Nil(t, err)

	mgr, err := NewManager("/tmp/mock", handlers, healthChan, manager...)
	assert.NotNil(t, mgr)
	assert.Nil(t, err)

	go mgr.Start()

	assert.Nil(t, mgr.loadContainers())
}

func TestStoreMetadata(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}

	metadata := &apipb.ContainerMetadata{
		ID:             "test-store-metadata-111111",
		RuntimeHandler: "runsc",
	}

	m.StoreMetadata(metadata.ID, metadata)

	assert.Equal(t, 1, m.containers.Count())
	assert.True(t, m.containers.Has(metadata.ID))
}

func TestSetResources(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}

	metadata := &apipb.ContainerMetadata{
		ID:             "test-set-resources",
		RuntimeHandler: "runsc",
	}
	m.StoreMetadata(metadata.ID, metadata)

	err := m.SetResources(metadata.ID, &runtimeapi.LinuxContainerResources{
		CpuShares:          256,
		MemoryLimitInBytes: 128 * 1024 * 1024,
	}, &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{CpuMilli: 250, MemoryBytes: 64 * 1024 * 1024}})
	assert.NoError(t, err)

	container, err := m.Get(metadata.ID)
	assert.NoError(t, err)
	assert.NotNil(t, container.Status.Get().LinuxResources)
	assert.Equal(t, uint64(256), container.Status.Get().LinuxResources.CpuShares)
	assert.Equal(t, int64(128*1024*1024), container.Status.Get().LinuxResources.MemoryLimitInBytes)
	assert.Equal(t, int64(64*1024*1024), container.Status.Get().ResourceSpec.GetRequests().GetMemoryBytes())
}

func TestLoadContainer(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}
	// Test loading failed caused by file not-exist
	_, err := m.loadContainer(t.TempDir())

	m1 := &Manager{
		root:        t.TempDir(),
		recyclePath: "/tmp/test-load-container/non-exist",
		containers:  cmap.New[*Container](),
	}
	assert.Error(t, err)

	// Test loading failed(file not-exist) and recycle failed
	_, err = m1.loadContainer(t.TempDir())
	assert.Error(t, err)
}

func TestLoadContainersUsesMetadataIdentityWithoutGeneratedPrefix(t *testing.T) {
	root := filepath.Join(t.TempDir(), "containers")
	writer := &Manager{
		root:        root,
		recyclePath: filepath.Join(t.TempDir(), "recycle"),
		containers:  cmap.New[*Container](),
	}
	writer.StoreMetadata("alloc-123", &apipb.ContainerMetadata{ID: "alloc-123", RuntimeHandler: "runsc"})

	reader := &Manager{root: root, containers: cmap.New[*Container]()}
	require.NoError(t, reader.loadContainers())
	assert.True(t, reader.containers.Has("alloc-123"))
}

func TestLoadContainerRejectsMetadataDirectoryMismatch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "containers")
	writer := &Manager{
		root:        root,
		recyclePath: filepath.Join(t.TempDir(), "recycle"),
		containers:  cmap.New[*Container](),
	}
	err := writer.StoreMetadata("directory-id", &apipb.ContainerMetadata{ID: "different-id", RuntimeHandler: "runsc"})
	require.ErrorContains(t, err, "does not match storage id")
}

func TestStartMonitorGoroutine(t *testing.T) {
	containers := cmap.New[*Container]()
	// Contains "success" to mock success
	id := "success-start-monitor-test1"

	container := &Container{
		Metadata: &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"},
		Spec:     &specs.Spec{},
	}
	containers.Set(id, container)

	serviceHandler := cmap.New[contract.RuntimeHandler]()
	r := runtimetest.NewFakeRuntimeHandler()
	serviceHandler.Set("runsc", r)

	m := &Manager{
		root:             t.TempDir(),
		recyclePath:      t.TempDir(),
		containers:       containers,
		monitorStopChan:  cmap.New[chan struct{}](),
		serviceHandler:   serviceHandler,
		resourceManagers: cmap.New[resourcemanager.Manager](),
		idGenerator:      truncindex.NewTruncGenerator("sandbox", []string{id}),
	}

	stop := make(chan struct{})

	m.startMonitorGoroutine(container.Metadata, stop)
	assert.NoError(t, m.Delete(id))

	select {
	case <-stop:
	case <-time.After(5 * time.Second):
		t.Error("start Monitor did not stop in time")
	}

}

func TestStartRecoveredMonitorsAfterInventoryReconciliation(t *testing.T) {
	containers := cmap.New[*Container]()
	containers.Set("live", &Container{Metadata: &apipb.ContainerMetadata{ID: "live", RuntimeHandler: "runsc"}})
	containers.Set("orphan", &Container{Metadata: &apipb.ContainerMetadata{ID: "orphan", RuntimeHandler: "runsc"}})

	handlers := cmap.New[contract.RuntimeHandler]()
	handlers.Set("runsc", runtimetest.NewFakeRuntimeHandler())
	m := &Manager{
		containers:      containers,
		serviceHandler:  handlers,
		monitorStopChan: cmap.New[chan struct{}](),
		syncEventChan:   make(chan Event, 8),
	}

	// Runtime inventory reconciliation removes the proven orphan before any
	// recovered Wait call can be started for it.
	m.containers.Remove("orphan")
	m.startRecoveredMonitors()

	assert.True(t, m.monitorStopChan.Has("live"))
	assert.False(t, m.monitorStopChan.Has("orphan"))
	m.stopMonitor("live")
}

func TestHousekeeping(t *testing.T) {
	healthChan := make(chan bool)

	m := &Manager{
		root:            t.TempDir(),
		recyclePath:     t.TempDir(),
		containers:      cmap.New[*Container](),
		serviceHandler:  cmap.New[contract.RuntimeHandler](),
		monitorStopChan: cmap.New[chan struct{}](),
		healthChan:      healthChan,
	}

	go m.housekeeping()
	go m.housekeeping()

	// Only receive once, it will block here if housekeeping run twice
	<-healthChan
	assert.Eventually(t, func() bool {
		return !m.isHousekeepingRunning.Load()
	}, time.Second, 10*time.Millisecond)

	// Mock housekeeping is running
	m.isHousekeepingRunning.Store(true)
	m.housekeeping() // should return directly and not touch the isHousekeepingRunning
	assert.True(t, m.isHousekeepingRunning.Load())

}

func TestManagerStopIsIdempotent(t *testing.T) {
	resourceManager := &stopTestResourceManager{}
	m := &Manager{
		resourceManagers: cmap.New[resourcemanager.Manager](),
		stopChan:         make(chan struct{}),
	}
	m.resourceManagers.Set(string(resourceManager.ResourceName()), resourceManager)

	m.Stop()
	m.Stop()

	assert.Equal(t, 1, resourceManager.shutdowns)
}
