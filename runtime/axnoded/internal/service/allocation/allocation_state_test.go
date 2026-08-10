package allocation

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type failingAllocationStateStore struct{ testStateStore }

func (s failingAllocationStateStore) PutRecord(bucket, key string, value proto.Message) error {
	if bucket == config.AllocationStateBucket {
		return errors.New("allocation state store unavailable")
	}
	return s.testStateStore.PutRecord(bucket, key, value)
}

type failingAllocationStateDeleteStore struct{ testStateStore }

func (s failingAllocationStateDeleteStore) DeleteRecord(bucket, key string) error {
	if bucket == config.AllocationStateBucket {
		return errors.New("allocation state delete unavailable")
	}
	return s.testStateStore.DeleteRecord(bucket, key)
}

type corruptAllocationStateStore struct{ testStateStore }

func (s corruptAllocationStateStore) ForEachRecord(bucket string, visit func(key string, value []byte) error) error {
	if bucket == config.AllocationStateBucket {
		if err := visit("corrupt-allocation", []byte("not-a-protobuf")); err != nil {
			return err
		}
	}
	return s.testStateStore.ForEachRecord(bucket, visit)
}

type countingAllocationStateStore struct {
	testStateStore
	puts    atomic.Int64
	deletes atomic.Int64
}

func (s *countingAllocationStateStore) PutRecord(bucket, key string, value proto.Message) error {
	if bucket == config.AllocationStateBucket {
		s.puts.Add(1)
	}
	return s.testStateStore.PutRecord(bucket, key, value)
}

func (s *countingAllocationStateStore) DeleteRecord(bucket, key string) error {
	if bucket == config.AllocationStateBucket {
		s.deletes.Add(1)
	}
	return s.testStateStore.DeleteRecord(bucket, key)
}

func persistedAllocationState(t *testing.T, store stateStore, allocationID string, images ...string) {
	t.Helper()
	now := time.Now().UTC()
	record := &apipb.AllocationState{
		AllocationID:    allocationID,
		RuntimeTemplate: testRuntimeTemplate(t, "runtime-"+allocationID),
		ImageMountUrls:  images,
		EnforcementManifest: &apipb.AllocationEnforcementManifest{
			RuntimeName: "runsc", BundlePath: "/var/lib/axnoded/root/containers/" + allocationID,
			CreatedAtUnixNano: now.UnixNano(),
		},
		LaunchVerification: &apipb.AllocationLaunchVerification{VerifiedAtUnixNano: now.UnixNano()},
	}
	if err := store.PutRecord(config.AllocationStateBucket, allocationID, record); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllocationStatesRejectsMissingAtomicLaunchProof(t *testing.T) {
	store := storetest.NewMockStore()
	const allocationID = "missing-launch-proof"
	record := &apipb.AllocationState{
		AllocationID: allocationID, RuntimeTemplate: testRuntimeTemplate(t, "runtime-"+allocationID),
		EnforcementManifest: &apipb.AllocationEnforcementManifest{
			RuntimeName: "runsc", BundlePath: "/var/lib/axnoded/root/containers/" + allocationID,
			CreatedAtUnixNano: time.Now().UnixNano(),
		},
	}
	if err := store.PutRecord(config.AllocationStateBucket, allocationID, record); err != nil {
		t.Fatal(err)
	}
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{ID: allocationID, RuntimeHandler: "runsc"})
	time.Sleep(100 * time.Millisecond)
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{allocationID: {}}); err == nil {
		t.Fatal("loadAllocationStates() accepted a live allocation without atomic launch verification")
	}
}

func TestValidateRecoveredManagedAllocationRequiresDurableCapabilityConditionSet(t *testing.T) {
	now := time.Now().UTC()
	manifest := &apipb.AllocationEnforcementManifest{
		RuntimeName: "runsc", BundlePath: "/var/lib/axnoded/root/containers/managed-condition-recovery",
		CreatedAtUnixNano: now.UnixNano(),
	}
	verification, err := newLaunchVerification(manifest, nil, now, now)
	if err != nil {
		t.Fatal(err)
	}
	record := &apipb.AllocationState{
		AllocationID: "managed-condition-recovery", AllocationAttempt: 1,
		AllocationRequestDigest: testAllocationRequestDigest,
		EnforcementManifest:     manifest, LaunchVerification: verification,
	}
	if err := validateRecoveredCapabilityState(record, now); err == nil {
		t.Fatal("validateRecoveredCapabilityState() accepted a managed allocation without a condition set")
	}
	record.CapabilityConditions = &capabilityv1.CapabilityConditionSet{
		Revision: 1, ObservedAt: timestamppb.New(now),
	}
	record.CapabilityAdmissionConditions = &capabilityv1.CapabilityConditionSet{
		Revision: 2, ObservedAt: timestamppb.New(now),
	}
	record.AllocationRequestDigest = ""
	if err := validateRecoveredCapabilityState(record, now); err == nil {
		t.Fatal("validateRecoveredCapabilityState() accepted a managed allocation without a request digest")
	}
	record.AllocationRequestDigest = testAllocationRequestDigest
	if err := validateRecoveredCapabilityState(record, now); err != nil {
		t.Fatalf("validateRecoveredCapabilityState() rejected an atomic empty admission: %v", err)
	}
	record.CapabilityAdmissionConditions = nil
	if err := validateRecoveredCapabilityState(record, now); err == nil {
		t.Fatal("validateRecoveredCapabilityState() accepted a managed allocation without sealed create proof")
	}
}

func TestLoadAllocationStatesRestoresLiveContainerMountOwnership(t *testing.T) {
	store := storetest.NewMockStore()
	allocationID := "image-resource-recovery"
	imageURL := "example.local/tools@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	persistedAllocationState(t, store, allocationID, imageURL, imageURL)

	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{ID: allocationID, RuntimeHandler: "runsc"})
	time.Sleep(100 * time.Millisecond)
	mounter := &imageMountTestMounter{imagePaths: map[string]string{imageURL: filepath.Join(t.TempDir(), "rootfs")}}
	fixture.lrtManager = langruntime.NewLanguageRuntimeManager(mounter)
	fixture.controller.lrtManager = fixture.lrtManager
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{allocationID: {}}); err != nil {
		t.Fatal(err)
	}

	fixture.controller.stateMu.RLock()
	state := fixture.controller.allocationStates[allocationID]
	fixture.controller.stateMu.RUnlock()
	if state == nil || len(state.imageMountRoots) != 2 || len(state.record.GetImageMountUrls()) != 2 {
		t.Fatalf("restored state = %+v", state)
	}
	if err := fixture.lrtManager.ReconcileMountLeases(); err != nil {
		t.Fatal(err)
	}
	if len(mounter.reconciled) != 1 || mounter.reconciled[0] != "test-lease:"+imageURL {
		t.Fatalf("reconciled leases = %+v", mounter.reconciled)
	}
	if err := fixture.controller.releaseAllocationState(allocationID); err != nil {
		t.Fatal(err)
	}
	imageUnmounts := 0
	for _, unmounted := range mounter.umounts {
		if unmounted.SrcType == apipb.RootfsSrcType_IMAGE {
			imageUnmounts++
		}
	}
	if imageUnmounts != 1 {
		t.Fatalf("image resource unmounts = %d, want one shared-rootfs release", imageUnmounts)
	}
}

func TestLoadAllocationStatesDeletesOrphanRecord(t *testing.T) {
	store := storetest.NewMockStore()
	persistedAllocationState(t, store, "missing", "example.local/missing:latest")
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{}); err != nil {
		t.Fatal(err)
	}
	var record apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, "missing", &record); err == nil {
		t.Fatal("orphan allocation state was retained")
	}
}

func TestRestoreAllocationStateSkipsDestructiveReconcileAfterLiveRecoveryFailure(t *testing.T) {
	store := storetest.NewMockStore()
	allocationID := "image-resource-recovery-failure"
	imageURL := "example.local/tools@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	persistedAllocationState(t, store, allocationID, imageURL)
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{ID: allocationID, RuntimeHandler: "runsc"})
	time.Sleep(100 * time.Millisecond)
	mounter := &imageMountTestMounter{mountErr: errors.New("imagemgr unavailable")}
	fixture.lrtManager = langruntime.NewLanguageRuntimeManager(mounter)
	fixture.controller.lrtManager = fixture.lrtManager
	if err := fixture.controller.RestoreAllocationState(map[string]struct{}{allocationID: {}}); err == nil {
		t.Fatal("RestoreAllocationState() succeeded with incomplete live state")
	}
	if mounter.reconciled != nil {
		t.Fatalf("reconciled incomplete desired set: %+v", mounter.reconciled)
	}
	var persisted apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &persisted); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAllocationStatesRetainsPartialRecoveryForLiveContainer(t *testing.T) {
	store := storetest.NewMockStore()
	allocationID := "partial-image-resource-recovery"
	firstImage := "example.local/first@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	secondImage := "example.local/second@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	persistedAllocationState(t, store, allocationID, firstImage, secondImage)
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{ID: allocationID, RuntimeHandler: "runsc"})
	time.Sleep(100 * time.Millisecond)
	mounter := &imageMountTestMounter{
		imagePaths: map[string]string{firstImage: filepath.Join(t.TempDir(), "rootfs")},
		mountErrs:  map[string]error{secondImage: errors.New("second image unavailable")},
	}
	fixture.lrtManager = langruntime.NewLanguageRuntimeManager(mounter)
	fixture.controller.lrtManager = fixture.lrtManager
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{allocationID: {}}); err == nil {
		t.Fatal("loadAllocationStates() succeeded with an incomplete live recovery")
	}
	fixture.controller.stateMu.RLock()
	state := fixture.controller.allocationStates[allocationID]
	fixture.controller.stateMu.RUnlock()
	if state == nil || len(state.imageMountRoots) != 1 {
		t.Fatalf("partially recovered state = %+v", state)
	}
	if len(mounter.umounts) != 0 {
		t.Fatalf("partially recovered live root was unmounted: %+v", mounter.umounts)
	}
}

func TestImageMountAcquireRollsBackWhenOwnershipPersistenceFails(t *testing.T) {
	imageURL := "example.local/tools@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	}, failingAllocationStateStore{testStateStore: storetest.NewMockStore()}, fakeVolumePublisher{})
	mounter := &imageMountTestMounter{imagePaths: map[string]string{imageURL: filepath.Join(t.TempDir(), "rootfs")}}
	fixture.lrtManager = langruntime.NewLanguageRuntimeManager(mounter)
	fixture.controller.lrtManager = fixture.lrtManager
	_, err := fixture.controller.Start(context.Background(), &apipb.StartRequest{
		ContainerID: allocationIDForTest(t),
		RuntimeTemplate: &apipb.RuntimeTemplate{
			ID:      "persistence-failure-runtime",
			Sandbox: "runsc",
			Rootfs:  &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
			Command: []string{"/bin/sh"},
		},
		ImageMounts: []*apipb.ImageMount{{Image: imageURL, Target: "/opt/tool"}},
	})
	if err == nil {
		t.Fatal("Start() succeeded when ownership persistence failed")
	}
	if handler.createCalls != 0 {
		t.Fatalf("runtime create calls = %d, want 0 before durable state", handler.createCalls)
	}
	imageUnmounts := 0
	for _, unmounted := range mounter.umounts {
		if unmounted.SrcType == apipb.RootfsSrcType_IMAGE {
			imageUnmounts++
		}
	}
	if imageUnmounts != 1 {
		t.Fatalf("image resource unmounts = %d, want rollback", imageUnmounts)
	}
}

func TestReleaseAllocationStatePreservesRuntimeWhenDeletePersistenceFails(t *testing.T) {
	store := failingAllocationStateDeleteStore{testStateStore: storetest.NewMockStore()}
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	runtime := addTestRuntimeMappingRuntime(t, fixture.lrtManager, testRuntimeTemplate(t, "delete-failure-runtime"))
	runtime.IncRef()
	if err := fixture.controller.rememberContainerRuntime("delete-failure", runtime); err != nil {
		t.Fatal(err)
	}
	if err := fixture.controller.releaseAllocationState("delete-failure"); err == nil {
		t.Fatal("releaseAllocationState() succeeded when durable delete failed")
	}
	if _, ok := fixture.controller.runtimeMapping("delete-failure"); !ok {
		t.Fatal("runtime reference was released before durable state deletion")
	}
}

func TestLoadAllocationStatesIsolatesCorruptRecordAndRestoresValidRecord(t *testing.T) {
	base := storetest.NewMockStore()
	allocationID := "valid-allocation"
	persistedAllocationState(t, base, allocationID)
	store := corruptAllocationStateStore{testStateStore: base}
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{ID: allocationID, RuntimeHandler: "runsc"})
	fixture.manager.StoreMetadata("corrupt-allocation", &apipb.ContainerMetadata{ID: "corrupt-allocation", RuntimeHandler: "runsc"})
	time.Sleep(100 * time.Millisecond)
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{allocationID: {}, "corrupt-allocation": {}}); err == nil {
		t.Fatal("loadAllocationStates() succeeded with a corrupt record")
	}
	if _, ok := fixture.controller.runtimeMapping(allocationID); !ok {
		t.Fatal("valid allocation was not restored beside corrupt record")
	}
}

func TestAllocationRecordsDeleteIndependently(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	}, store, fakeVolumePublisher{})
	for _, allocationID := range []string{"allocation-a", "allocation-b"} {
		runtime := addTestRuntimeMappingRuntime(t, fixture.lrtManager, testRuntimeTemplate(t, "runtime-"+allocationID))
		runtime.IncRef()
		if err := fixture.controller.rememberContainerRuntime(allocationID, runtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.controller.releaseAllocationState("allocation-a"); err != nil {
		t.Fatal(err)
	}
	var remaining apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, "allocation-b", &remaining); err != nil {
		t.Fatalf("unrelated allocation record was removed: %v", err)
	}
}

func TestStartAndDeleteUseOneAllocationTransactionEach(t *testing.T) {
	store := &countingAllocationStateStore{testStateStore: storetest.NewMockStore()}
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": handler}, store, fakeVolumePublisher{})
	imageURL := "example.local/atomic@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	mounter := &imageMountTestMounter{imagePaths: map[string]string{imageURL: t.TempDir()}}
	fixture.lrtManager = langruntime.NewLanguageRuntimeManager(mounter)
	fixture.controller.lrtManager = fixture.lrtManager
	allocationID := "atomic-allocation-state"
	if _, err := fixture.controller.Start(context.Background(), &apipb.StartRequest{
		ContainerID: allocationID,
		RuntimeTemplate: &apipb.RuntimeTemplate{
			ID:      "atomic-runtime",
			Sandbox: "runsc",
			Rootfs:  &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
			Command: []string{"/bin/sh"},
		},
		ImageMounts: []*apipb.ImageMount{{Image: imageURL, Target: "/opt/tool"}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := store.puts.Load(); got != 1 {
		t.Fatalf("allocation writes after start = %d, want 1", got)
	}
	var record apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &record); err != nil {
		t.Fatal(err)
	}
	if record.GetRuntimeTemplate() == nil || len(record.GetImageMountUrls()) != 1 {
		t.Fatalf("persisted aggregate state = %+v", &record)
	}
	if _, err := fixture.controller.Delete(context.Background(), &apipb.DeleteRequest{ID: allocationID}); err != nil {
		t.Fatal(err)
	}
	if got := store.deletes.Load(); got != 1 {
		t.Fatalf("allocation deletes after teardown = %d, want 1", got)
	}
}

func allocationIDForTest(t *testing.T) string {
	t.Helper()
	return "persistence-failure"
}
