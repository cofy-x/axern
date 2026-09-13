package allocation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
)

func testEnvironmentTemplate(t *testing.T, id string) *apipb.EnvironmentTemplate {
	t.Helper()
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0o755))
	return &apipb.EnvironmentTemplate{
		ID:     id,
		Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: rootfsDir}},
		Argv:   []string{"/bin/sh"},
	}
}

func addTestRuntimeMappingRuntime(t *testing.T, manager *environmentcache.EnvironmentCache, template *apipb.EnvironmentTemplate) *environmentcache.PreparedEnvironment {
	t.Helper()
	config, err := environmentcache.RootfsConfigFromEnvironmentTemplate(template)
	assert.NoError(t, err)
	result, err := manager.PrepareEnvironment(t.Context(), template, config)
	assert.NoError(t, err)
	return result.Environment
}

func TestAllocationRuntimeStateRoundTrip(t *testing.T) {
	store := storetest.NewMockStore()
	first := newTestAllocationControllerWithStore(t, runtimetest.NewFakeSandboxRuntime(), store)
	template := testEnvironmentTemplate(t, "allocation-runtime")
	runtime := addTestRuntimeMappingRuntime(t, first.environmentCache, template)
	allocationID := "allocation-runtime-round-trip"
	assert.NoError(t, first.controller.BindControlPlaneAllocation(allocationID, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	err := first.controller.StoreCapabilityRequirements(allocationID, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil)
	assert.NoError(t, err)
	runtime.IncRef()
	assert.NoError(t, first.controller.rememberContainerRuntime(allocationID, runtime))
	now := time.Now().UTC()
	assert.NoError(t, first.controller.StoreLaunchVerification(allocationID, &apipb.AllocationEnforcementManifest{
		BundlePath:        "/var/lib/axnoded/root/containers/" + allocationID,
		CreatedAtUnixNano: now.UnixNano(),
	}, nil, now))
	var persisted apipb.AllocationState
	assert.NoError(t, store.GetRecord(config.AllocationStateBucket, allocationID, &persisted))
	assert.Equal(t, template.GetID(), persisted.GetEnvironmentTemplate().GetID())

	second := newTestAllocationControllerWithStore(t, runtimetest.NewFakeSandboxRuntime(), store)
	second.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{})
	time.Sleep(200 * time.Millisecond)
	assert.NoError(t, second.controller.loadAllocationStates(map[string]struct{}{allocationID: {}}))
	restored, ok := second.controller.runtimeMapping(allocationID)
	assert.True(t, ok)
	assert.Equal(t, template.GetID(), restored.ID)
}

func TestLoadAllocationStatesSkipsOrphanContainers(t *testing.T) {
	store := storetest.NewMockStore()
	allocationID := "orphan-allocation"
	assert.NoError(t, store.PutRecord(config.AllocationStateBucket, allocationID, &apipb.AllocationState{
		AllocationID: allocationID, EnvironmentTemplate: testEnvironmentTemplate(t, "orphan-runtime"),
	}))
	fixture := newTestAllocationControllerWithStore(t, runtimetest.NewFakeSandboxRuntime(), store)
	assert.NoError(t, fixture.controller.loadAllocationStates(map[string]struct{}{}))
	_, ok := fixture.controller.runtimeMapping(allocationID)
	assert.False(t, ok)
	var persisted apipb.AllocationState
	assert.Error(t, store.GetRecord(config.AllocationStateBucket, allocationID, &persisted))
}

func TestLoadAllocationStatesEmptyStore(t *testing.T) {
	fixture := newTestAllocationController(t, runtimetest.NewFakeSandboxRuntime())
	assert.NoError(t, fixture.controller.loadAllocationStates(map[string]struct{}{}))
	assert.Zero(t, fixture.controller.runtimeMappingCount())
}

func TestLoadAllocationStatesRejectsLiveRuntimeWithoutRecoveryRecord(t *testing.T) {
	fixture := newTestAllocationController(t, runtimetest.NewFakeSandboxRuntime())
	err := fixture.controller.loadAllocationStates(map[string]struct{}{"missing-live-record": {}})
	assert.ErrorContains(t, err, "has no allocation recovery record")
	assert.Zero(t, fixture.controller.runtimeMappingCount())
}
