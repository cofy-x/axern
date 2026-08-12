/**
 * Alipay.com Inc.
 * Copyright (c) 2004-2024 All Rights Reserved.
 */

package container

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
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

type failingStatusStorage struct {
	status containerStatusForTest
	err    error
}

// containerStatusForTest keeps the failing storage's value private from the
// callback so a failed checkpoint cannot accidentally mutate it in place.
type containerStatusForTest struct{ value Status }

func (s *failingStatusStorage) Get() Status                 { return deepCopyOf(s.status.value) }
func (s *failingStatusStorage) UpdateSync(UpdateFunc) error { return s.err }
func (s *failingStatusStorage) Update(UpdateFunc) error     { return s.err }
func (s *failingStatusStorage) Delete() error               { return s.err }

type flakyStatusStorage struct {
	mu        sync.Mutex
	status    Status
	failures  int
	attempted chan struct{}
}

func (s *flakyStatusStorage) Get() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return deepCopyOf(s.status)
}

func (s *flakyStatusStorage) UpdateSync(update UpdateFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case s.attempted <- struct{}{}:
	default:
	}
	if s.failures > 0 {
		s.failures--
		return errors.New("temporary checkpoint failure")
	}
	next, err := update(s.status)
	if err == nil {
		s.status = next
	}
	return err
}

func (s *flakyStatusStorage) Update(update UpdateFunc) error { return s.UpdateSync(update) }
func (s *flakyStatusStorage) Delete() error                  { return nil }

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

func TestSyncRuntimeIdentityFromStateDoesNotReviveTerminalProcessIdentity(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}
	const id = "test-runtime-identity-111111"
	require.NoError(t, m.StoreMetadata(id, &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}))
	before, err := m.Get(id)
	require.NoError(t, err)
	originalStartedAt := before.Status.Get().StartedAt
	finishedAt := time.Now().UTC()
	require.NoError(t, m.SetExit(
		id,
		42,
		true,
		finishedAt,
		"exact runtime exit",
		commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED,
	))

	require.NoError(t, m.SyncRuntimeIdentityFromState(id, &contract.UnionContainerState{
		ID:             id,
		InitProcessPid: 321,
		Status:         contract.ContainerStatusExited,
		Created:        "2026-08-11T15:59:37Z",
	}))

	stored, err := m.Get(id)
	require.NoError(t, err)
	status := stored.Status.Get()
	assert.Equal(t, -1, status.Pid)
	assert.Equal(t, originalStartedAt, status.StartedAt)
	assert.Equal(t, finishedAt.Format(time.RFC3339Nano), status.FinishedAt)
	assert.Equal(t, int32(42), status.ExitCode)
	assert.True(t, status.ExitCodeKnown)
	assert.Equal(t, "exact runtime exit", status.Message)
	assert.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, status.DiagnosticCode)
}

func TestSyncRuntimeIdentityFromRunningStateEnrichesLocalIdentity(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}
	const id = "test-running-identity-111111"
	require.NoError(t, m.StoreMetadata(id, &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}))
	require.NoError(t, m.SyncRuntimeIdentityFromState(id, &contract.UnionContainerState{
		ID:             id,
		InitProcessPid: 321,
		Status:         contract.ContainerStatusRunning,
		Created:        "2026-08-11T15:59:37Z",
	}))

	stored, err := m.Get(id)
	require.NoError(t, err)
	status := stored.Status.Get()
	assert.Equal(t, 321, status.Pid)
	assert.Equal(t, "2026-08-11T15:59:37Z", status.StartedAt)
	assert.Empty(t, status.FinishedAt)
}

func TestPersistMonitorExitClassifiesBeforeCheckpoint(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}
	const id = "test-memory-oom-111111"
	require.NoError(t, m.StoreMetadata(id, &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}))
	m.SetExitClassifier(func(Event) (commonv1.WorkloadDiagnosticCode, string) {
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED,
			"sandbox memory limit exceeded"
	})

	classified, err := m.persistMonitorExit(Event{
		Type: EventTypeExit, ContainerID: id, ExitCode: 137, ExitCodeKnown: true, ExitedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, classified.DiagnosticCode)

	stored, err := m.Get(id)
	require.NoError(t, err)
	status := stored.Status.Get()
	assert.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, status.DiagnosticCode)
	assert.Equal(t, "sandbox memory limit exceeded", status.Message)
	assert.Equal(t, int32(137), status.ExitCode)
	assert.True(t, status.ExitCodeKnown)

	reloaded, err := LoadStatus(filepath.Join(m.root, id))
	require.NoError(t, err)
	assert.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, reloaded.Get().DiagnosticCode)
}

func TestPersistMonitorExitNormalizesMissingRuntimeTimestamp(t *testing.T) {
	m := &Manager{
		root:        t.TempDir(),
		recyclePath: t.TempDir(),
		containers:  cmap.New[*Container](),
	}
	const id = "test-zero-exit-time-111111"
	require.NoError(t, m.StoreMetadata(id, &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}))

	classified, err := m.persistMonitorExit(Event{Type: EventTypeExit, ContainerID: id, ExitCodeKnown: true})
	require.NoError(t, err)
	assert.False(t, classified.ExitedAt.IsZero())
	stored, err := m.Get(id)
	require.NoError(t, err)
	assert.Equal(t, classified.ExitedAt, ParseTimestampTime(stored.Status.Get().FinishedAt))
}

func TestSetExitPropagatesCheckpointFailure(t *testing.T) {
	wantErr := errors.New("durable status unavailable")
	const id = "checkpoint-failure-111111"
	m := &Manager{containers: cmap.New[*Container]()}
	m.containers.Set(id, &Container{
		Metadata: &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"},
		Status:   &failingStatusStorage{err: wantErr},
	})

	err := m.SetExit(id, 137, true, time.Now().UTC(), "oom", commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED)
	require.ErrorIs(t, err, wantErr)
}

func TestPersistMonitorExitRetriesTransientCheckpointFailure(t *testing.T) {
	const id = "checkpoint-retry-111111"
	storage := &flakyStatusStorage{failures: 1, attempted: make(chan struct{}, 2)}
	m := &Manager{containers: cmap.New[*Container]()}
	m.containers.Set(id, &Container{
		Metadata: &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"},
		Status:   storage,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	classified, err := m.persistMonitorExitWithRetry(ctx, Event{
		Type: EventTypeExit, ContainerID: id, ExitCode: 0, ExitCodeKnown: true, ExitedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	assert.Equal(t, id, classified.ContainerID)
	assert.Equal(t, int32(0), storage.Get().ExitCode)
	assert.True(t, storage.Get().ExitCodeKnown)
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
		PATH:     t.TempDir(),
	}
	container.Status, _ = LoadStatus(container.PATH)
	containers.Set(id, container)

	serviceHandler := cmap.New[contract.RuntimeHandler]()
	r := runtimetest.NewFakeRuntimeHandler()
	serviceHandler.Set("runsc", r)

	m := &Manager{
		root:             t.TempDir(),
		recyclePath:      t.TempDir(),
		containers:       containers,
		monitors:         cmap.New[*containerMonitor](),
		serviceHandler:   serviceHandler,
		resourceManagers: cmap.New[resourcemanager.Manager](),
		idGenerator:      truncindex.NewTruncGenerator("sandbox", []string{id}),
	}

	require.NoError(t, m.StartMonitor(container.Metadata))
	assert.NoError(t, m.Delete(id))
	assert.False(t, m.monitors.Has(id))

}

func TestStartMonitorRejectsIncompleteMetadataAndUnknownRuntime(t *testing.T) {
	m := &Manager{
		monitors:       cmap.New[*containerMonitor](),
		serviceHandler: cmap.New[contract.RuntimeHandler](),
	}
	require.ErrorContains(t, m.StartMonitor(nil), "complete metadata")
	require.ErrorContains(t, m.StartMonitor(&apipb.ContainerMetadata{ID: "missing-runtime", RuntimeHandler: "runsc"}), "not found")
}

func TestStartMonitorRejectsMissingDurableContainerRecord(t *testing.T) {
	handlers := cmap.New[contract.RuntimeHandler]()
	handlers.Set("runsc", runtimetest.NewFakeRuntimeHandler())
	m := &Manager{
		containers:     cmap.New[*Container](),
		monitors:       cmap.New[*containerMonitor](),
		serviceHandler: handlers,
	}
	require.ErrorContains(t, m.StartMonitor(&apipb.ContainerMetadata{ID: "missing-record", RuntimeHandler: "runsc"}), "durable status record")
}

func TestStartMonitorRejectsDurableRuntimeOwnershipMismatch(t *testing.T) {
	const id = "runtime-mismatch-111111"
	handlers := cmap.New[contract.RuntimeHandler]()
	handlers.Set("runc", runtimetest.NewFakeRuntimeHandler())
	m := &Manager{
		containers:     cmap.New[*Container](),
		monitors:       cmap.New[*containerMonitor](),
		serviceHandler: handlers,
	}
	m.containers.Set(id, &Container{
		Metadata: &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"},
		Status:   &flakyStatusStorage{status: Status{StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}, attempted: make(chan struct{}, 1)},
	})
	require.ErrorContains(t, m.StartMonitor(&apipb.ContainerMetadata{ID: id, RuntimeHandler: "runc"}), "does not match")
}

func TestStartMonitorDoesNotRestartDurableTerminalContainer(t *testing.T) {
	const id = "terminal-monitor-111111"
	handlers := cmap.New[contract.RuntimeHandler]()
	handlers.Set("runsc", runtimetest.NewFakeRuntimeHandler())
	m := &Manager{
		containers:     cmap.New[*Container](),
		monitors:       cmap.New[*containerMonitor](),
		serviceHandler: handlers,
	}
	m.containers.Set(id, &Container{
		Metadata: &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"},
		Status:   &statusStorage{status: Status{FinishedAt: time.Now().UTC().Format(time.RFC3339Nano)}},
	})
	require.NoError(t, m.StartMonitor(m.containers.Items()[id].Metadata))
	assert.False(t, m.monitors.Has(id))
}

func TestMonitorExitBarrierRequiresMonitorOrDurableTerminalCheckpoint(t *testing.T) {
	const id = "missing-monitor-111111"
	m := &Manager{
		root:       t.TempDir(),
		containers: cmap.New[*Container](),
		monitors:   cmap.New[*containerMonitor](),
	}
	require.NoError(t, m.StoreMetadata(id, &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}))
	require.ErrorContains(t, m.waitMonitorExitBarrier(id, time.Second), "neither an active monitor nor a durable terminal exit checkpoint")
	require.NoError(t, m.SetExit(id, 0, true, time.Now().UTC(), "", commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED))
	require.NoError(t, m.waitMonitorExitBarrier(id, time.Second))
}

func TestStartRecoveredMonitorsAfterInventoryReconciliation(t *testing.T) {
	containers := cmap.New[*Container]()
	containers.Set("live", &Container{Metadata: &apipb.ContainerMetadata{ID: "live", RuntimeHandler: "runsc"}, Status: &flakyStatusStorage{status: Status{StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}, attempted: make(chan struct{}, 1)}})
	containers.Set("orphan", &Container{Metadata: &apipb.ContainerMetadata{ID: "orphan", RuntimeHandler: "runsc"}, Status: &flakyStatusStorage{status: Status{StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}, attempted: make(chan struct{}, 1)}})

	handlers := cmap.New[contract.RuntimeHandler]()
	handlers.Set("runsc", runtimetest.NewFakeRuntimeHandler())
	m := &Manager{
		containers:     containers,
		serviceHandler: handlers,
		monitors:       cmap.New[*containerMonitor](),
		syncEventChan:  make(chan Event, 8),
	}

	// Runtime inventory reconciliation removes the proven orphan before any
	// recovered Wait call can be started for it.
	m.containers.Remove("orphan")
	m.startRecoveredMonitors()

	assert.True(t, m.monitors.Has("live"))
	assert.False(t, m.monitors.Has("orphan"))
	m.stopMonitor("live")
}

func TestHousekeeping(t *testing.T) {
	healthChan := make(chan bool)

	m := &Manager{
		root:           t.TempDir(),
		recyclePath:    t.TempDir(),
		containers:     cmap.New[*Container](),
		serviceHandler: cmap.New[contract.RuntimeHandler](),
		monitors:       cmap.New[*containerMonitor](),
		healthChan:     healthChan,
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
		monitors:         cmap.New[*containerMonitor](),
		stopChan:         make(chan struct{}),
	}
	m.resourceManagers.Set(string(resourceManager.ResourceName()), resourceManager)

	require.NoError(t, m.Stop(context.Background()))
	require.NoError(t, m.Stop(context.Background()))

	assert.Equal(t, 1, resourceManager.shutdowns)
}

func TestManagerStopHonorsDeadlineAndCanResumeMonitorJoin(t *testing.T) {
	handlers := cmap.New[contract.RuntimeHandler]()
	handlers.Set("runsc", runtimetest.NewFakeRuntimeHandler())
	m, err := NewManager(t.TempDir(), handlers, make(chan bool, 1))
	require.NoError(t, err)

	const id = "stop-monitor-join-111111"
	require.NoError(t, m.StoreMetadata(id, &apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}))
	observerEntered := make(chan struct{})
	releaseObserver := make(chan struct{})
	m.SetExitObserver(func(Event) error {
		close(observerEntered)
		<-releaseObserver
		return nil
	})
	require.NoError(t, m.StartMonitor(m.containers.Items()[id].Metadata))
	select {
	case <-observerEntered:
	case <-time.After(time.Second):
		t.Fatal("monitor observer did not start")
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	require.ErrorIs(t, m.Stop(stopCtx), context.DeadlineExceeded)
	cancelStop()
	close(releaseObserver)
	joinCtx, cancelJoin := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, m.Stop(joinCtx))
	cancelJoin()
	require.ErrorContains(t, m.StartMonitor(&apipb.ContainerMetadata{ID: id, RuntimeHandler: "runsc"}), "stopped")
}
