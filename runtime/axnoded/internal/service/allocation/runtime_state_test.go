package allocation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
)

func testRuntimeTemplate(t *testing.T, id string) *apipb.RuntimeTemplate {
	t.Helper()
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0o755))
	return &apipb.RuntimeTemplate{
		ID: id, Sandbox: "runsc",
		Rootfs:  &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: rootfsDir}},
		Command: []string{"/bin/sh"},
	}
}

func addTestRuntimeMappingRuntime(t *testing.T, manager *langruntime.LangRTManager, template *apipb.RuntimeTemplate) *langruntime.LanguageRuntime {
	t.Helper()
	config, err := langruntime.RootfsConfigFromRuntimeTemplate(template)
	assert.NoError(t, err)
	result, err := manager.AddLangRuntime(t.Context(), template, config, true)
	assert.NoError(t, err)
	return result.Runtime
}

func TestAllocationRuntimeStateRoundTrip(t *testing.T) {
	store := storetest.NewMockStore()
	first := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	template := testRuntimeTemplate(t, "allocation-runtime")
	runtime := addTestRuntimeMappingRuntime(t, first.lrtManager, template)
	allocationID := "allocation-runtime-round-trip"
	runtime.IncRef()
	assert.NoError(t, first.controller.rememberContainerRuntime(allocationID, runtime))
	var persisted apipb.AllocationState
	assert.NoError(t, store.GetRecord(config.AllocationStateBucket, allocationID, &persisted))
	assert.Equal(t, template.GetID(), persisted.GetRuntimeTemplate().GetID())

	second := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	second.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{ID: allocationID, RuntimeHandler: "runsc"})
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
		AllocationID: allocationID, RuntimeTemplate: testRuntimeTemplate(t, "orphan-runtime"),
	}))
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	assert.NoError(t, fixture.controller.loadAllocationStates(map[string]struct{}{}))
	_, ok := fixture.controller.runtimeMapping(allocationID)
	assert.False(t, ok)
	var persisted apipb.AllocationState
	assert.Error(t, store.GetRecord(config.AllocationStateBucket, allocationID, &persisted))
}

func TestLoadAllocationStatesEmptyStore(t *testing.T) {
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()})
	assert.NoError(t, fixture.controller.loadAllocationStates(map[string]struct{}{}))
	assert.Zero(t, fixture.controller.runtimeMappingCount())
}

func TestLoadAllocationStatesRejectsLiveRuntimeWithoutRecoveryRecord(t *testing.T) {
	fixture := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()})
	err := fixture.controller.loadAllocationStates(map[string]struct{}{"missing-live-record": {}})
	assert.ErrorContains(t, err, "has no allocation recovery record")
	assert.Zero(t, fixture.controller.runtimeMappingCount())
}
