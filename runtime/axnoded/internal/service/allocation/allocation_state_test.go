package allocation

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
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
		AllocationID:            allocationID,
		AllocationRequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EnvironmentTemplate:     testEnvironmentTemplate(t, "runtime-"+allocationID),
		ImageMountUrls:          images,
		EnforcementManifest: &apipb.AllocationEnforcementManifest{
			BundlePath:        "/var/lib/axnoded/root/containers/" + allocationID,
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
		AllocationID: allocationID, EnvironmentTemplate: testEnvironmentTemplate(t, "runtime-"+allocationID),
		EnforcementManifest: &apipb.AllocationEnforcementManifest{
			BundlePath:        "/var/lib/axnoded/root/containers/" + allocationID,
			CreatedAtUnixNano: time.Now().UnixNano(),
		},
	}
	if err := store.PutRecord(config.AllocationStateBucket, allocationID, record); err != nil {
		t.Fatal(err)
	}
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{})
	time.Sleep(100 * time.Millisecond)
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{allocationID: {}}); err == nil {
		t.Fatal("loadAllocationStates() accepted a live allocation without atomic launch verification")
	}
}

func TestInspectRecoveryRecordsClassifiesInterruptedCreateIntent(t *testing.T) {
	store := storetest.NewMockStore()
	const allocationID = "interrupted-create"
	record := &apipb.AllocationState{
		AllocationID:            allocationID,
		AllocationRequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := store.PutRecord(config.AllocationStateBucket, allocationID, record); err != nil {
		t.Fatal(err)
	}
	fixture := newTestAllocationControllerWithStore(t, runtimetest.NewFakeSandboxRuntime(), store)

	recovery, err := fixture.controller.InspectRecoveryRecords()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := recovery.Intents[allocationID]; !ok {
		t.Fatal("interrupted create intent was not returned for ordered cleanup")
	}
	if _, ok := recovery.LaunchVerified[allocationID]; ok {
		t.Fatal("interrupted create intent was classified as launch verified")
	}
}

func TestInspectRecoveryRecordsRejectsPartiallyPersistedLaunchVerification(t *testing.T) {
	store := storetest.NewMockStore()
	const allocationID = "partial-launch-verification"
	record := &apipb.AllocationState{
		AllocationID:            allocationID,
		AllocationRequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EnforcementManifest: &apipb.AllocationEnforcementManifest{
			BundlePath:        "/var/lib/axnoded/root/containers/" + allocationID,
			CreatedAtUnixNano: time.Now().UnixNano(),
		},
	}
	if err := store.PutRecord(config.AllocationStateBucket, allocationID, record); err != nil {
		t.Fatal(err)
	}
	fixture := newTestAllocationControllerWithStore(t, runtimetest.NewFakeSandboxRuntime(), store)

	if _, err := fixture.controller.InspectRecoveryRecords(); err == nil {
		t.Fatal("partially persisted launch verification was accepted")
	}
}

func TestInspectRecoveryRecordsRejectsBindingRequestMismatch(t *testing.T) {
	store := storetest.NewMockStore()
	const allocationID = "mismatched-binding"
	record := &apipb.AllocationState{
		AllocationID:            allocationID,
		AllocationRequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if err := store.PutRecord(config.AllocationStateBucket, allocationID, record); err != nil {
		t.Fatal(err)
	}
	fixture := newTestAllocationControllerWithStore(t, runtimetest.NewFakeSandboxRuntime(), store)
	if err := fixture.controller.BindControlPlaneAllocation(allocationID, "node-a", "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.controller.InspectRecoveryRecords(); err == nil {
		t.Fatal("allocation state with a mismatched control-plane binding was accepted")
	}
}

func TestValidateRecoveredAllocationRebuildsCapabilityConditions(t *testing.T) {
	now := time.Now().UTC()
	manifest := &apipb.AllocationEnforcementManifest{
		BundlePath:        "/var/lib/axnoded/root/containers/condition-recovery",
		CreatedAtUnixNano: now.UnixNano(),
	}
	verification, err := newLaunchVerification(manifest, nil, nil, now, now)
	if err != nil {
		t.Fatal(err)
	}
	record := &apipb.AllocationState{
		AllocationID:            "condition-recovery",
		AllocationRequestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EnforcementManifest:     manifest, LaunchVerification: verification,
	}
	if err := validateRecoveredCapabilityState(record, now); err != nil {
		t.Fatalf("validateRecoveredCapabilityState() rejected rebuildable conditions: %v", err)
	}
	record.AllocationRequestDigest = ""
	if err := validateRecoveredCapabilityState(record, now); err == nil {
		t.Fatal("validateRecoveredCapabilityState() accepted an allocation without a request digest")
	}
}

func TestNewLaunchVerificationBindsVerifiedEgressCapability(t *testing.T) {
	now := time.Now().UTC()
	manifest := &apipb.AllocationEnforcementManifest{
		BundlePath:        "/var/lib/axnoded/root/containers/network-policy",
		CreatedAtUnixNano: now.UnixNano(),
	}
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT)
	dependencies := []*capabilityv1.CapabilityRequirement{{
		Key: key, LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP,
	}}
	if _, err := newLaunchVerification(manifest, []*capabilityv1.CapabilityKey{key}, dependencies, now, now); err != nil {
		t.Fatalf("newLaunchVerification() rejected verified strict egress capability: %v", err)
	}
}

func TestLoadAllocationStatesRestoresLiveContainerMountOwnership(t *testing.T) {
	store := storetest.NewMockStore()
	allocationID := "image-resource-recovery"
	imageURL := "example.local/tools@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	persistedAllocationState(t, store, allocationID, imageURL, imageURL)

	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{})
	time.Sleep(100 * time.Millisecond)
	mounter := &imageMountTestMounter{imagePaths: map[string]string{imageURL: filepath.Join(t.TempDir(), "rootfs")}}
	fixture.environmentCache = environmentcache.NewEnvironmentCache(mounter)
	fixture.controller.environmentCache = fixture.environmentCache
	if err := fixture.controller.loadAllocationStates(map[string]struct{}{allocationID: {}}); err != nil {
		t.Fatal(err)
	}

	fixture.controller.stateMu.RLock()
	state := fixture.controller.allocationStates[allocationID]
	fixture.controller.stateMu.RUnlock()
	if state == nil || len(state.imageMountRoots) != 2 || len(state.record.GetImageMountUrls()) != 2 {
		t.Fatalf("restored state = %+v", state)
	}
	if err := fixture.environmentCache.ReconcileMountLeases(); err != nil {
		t.Fatal(err)
	}
	if len(mounter.reconciled) != 1 || mounter.reconciled[0] != "test-lease:"+imageURL {
		t.Fatalf("reconciled leases = %+v", mounter.reconciled)
	}
	if err := fixture.controller.releaseAllocationState(allocationID, false); err != nil {
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
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
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
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{})
	time.Sleep(100 * time.Millisecond)
	mounter := &imageMountTestMounter{mountErr: errors.New("imagemgr unavailable")}
	fixture.environmentCache = environmentcache.NewEnvironmentCache(mounter)
	fixture.controller.environmentCache = fixture.environmentCache
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
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{})
	time.Sleep(100 * time.Millisecond)
	mounter := &imageMountTestMounter{
		imagePaths: map[string]string{firstImage: filepath.Join(t.TempDir(), "rootfs")},
		mountErrs:  map[string]error{secondImage: errors.New("second image unavailable")},
	}
	fixture.environmentCache = environmentcache.NewEnvironmentCache(mounter)
	fixture.controller.environmentCache = fixture.environmentCache
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
	fixture := newTestAllocationControllerWithStore(t,
		handler,
		failingAllocationStateStore{testStateStore: storetest.NewMockStore()})
	mounter := &imageMountTestMounter{imagePaths: map[string]string{imageURL: filepath.Join(t.TempDir(), "rootfs")}}
	fixture.environmentCache = environmentcache.NewEnvironmentCache(mounter)
	fixture.controller.environmentCache = fixture.environmentCache
	if err := fixture.controller.BindControlPlaneAllocation(allocationIDForTest(t), "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	_, err := fixture.controller.Start(context.Background(), &apipb.StartRequest{
		ContainerID: allocationIDForTest(t),
		EnvironmentTemplate: &apipb.EnvironmentTemplate{
			ID:     "persistence-failure-runtime",
			Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
			Argv:   []string{"/bin/sh"},
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
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	if err := fixture.controller.BindControlPlaneAllocation("delete-failure", "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	runtime := addTestRuntimeMappingRuntime(t, fixture.environmentCache, testEnvironmentTemplate(t, "delete-failure-runtime"))
	runtime.IncRef()
	if err := fixture.controller.rememberContainerRuntime("delete-failure", runtime); err != nil {
		t.Fatal(err)
	}
	if err := fixture.controller.releaseAllocationState("delete-failure", false); err == nil {
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
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{})
	fixture.manager.StoreMetadata("corrupt-allocation", &apipb.ContainerMetadata{})
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
	fixture := newTestAllocationControllerWithStore(t,
		runtimetest.NewFakeSandboxRuntime(),
		store)
	for _, allocationID := range []string{"allocation-a", "allocation-b"} {
		if err := fixture.controller.BindControlPlaneAllocation(allocationID, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
			t.Fatal(err)
		}
		runtime := addTestRuntimeMappingRuntime(t, fixture.environmentCache, testEnvironmentTemplate(t, "runtime-"+allocationID))
		runtime.IncRef()
		if err := fixture.controller.rememberContainerRuntime(allocationID, runtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.controller.releaseAllocationState("allocation-a", false); err != nil {
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
	fixture := newTestAllocationControllerWithStore(t, handler, store)
	imageURL := "example.local/atomic@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	mounter := &imageMountTestMounter{imagePaths: map[string]string{imageURL: t.TempDir()}}
	fixture.environmentCache = environmentcache.NewEnvironmentCache(mounter)
	fixture.controller.environmentCache = fixture.environmentCache
	allocationID := "atomic-allocation-state"
	if err := fixture.controller.BindControlPlaneAllocation(allocationID, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.controller.Start(context.Background(), &apipb.StartRequest{
		ContainerID: allocationID,
		EnvironmentTemplate: &apipb.EnvironmentTemplate{
			ID:     "atomic-runtime",
			Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
			Argv:   []string{"/bin/sh"},
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
	if record.GetEnvironmentTemplate() == nil || len(record.GetImageMountUrls()) != 1 {
		t.Fatalf("persisted aggregate state = %+v", &record)
	}
	if _, err := fixture.controller.Delete(context.Background(), &apipb.DeleteRequest{ID: allocationID}); err != nil {
		t.Fatal(err)
	}
	if got := store.deletes.Load(); got != 1 {
		t.Fatalf("allocation deletes after teardown = %d, want 1", got)
	}
}

func TestTransientAllocationStateIsMemoryOnly(t *testing.T) {
	store := &countingAllocationStateStore{testStateStore: storetest.NewMockStore()}
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationControllerWithStore(t, handler, store)
	allocationID := "transient-allocation"
	if _, err := fixture.controller.Start(context.Background(), &apipb.StartRequest{
		ContainerID: allocationID,
		EnvironmentTemplate: &apipb.EnvironmentTemplate{
			ID:     "transient-environment",
			Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
			Argv:   []string{"/bin/sh"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if !fixture.controller.HasAllocation(allocationID) {
		t.Fatal("transient allocation state is unavailable in process")
	}
	if got := store.puts.Load(); got != 0 {
		t.Fatalf("transient allocation durable writes = %d, want 0", got)
	}
	var record apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &record); err == nil {
		t.Fatal("transient allocation wrote a durable recovery record")
	}
	if _, err := fixture.controller.Delete(context.Background(), &apipb.DeleteRequest{ID: allocationID}); err != nil {
		t.Fatal(err)
	}
	if got := store.deletes.Load(); got != 0 {
		t.Fatalf("transient allocation durable deletes = %d, want 0", got)
	}
}

func allocationIDForTest(t *testing.T) string {
	t.Helper()
	return "persistence-failure"
}
