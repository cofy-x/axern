package allocation

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const testAllocationRequestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCapabilityRecordMutationsPreserveConcurrentFields(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	controller := fixture.controller
	allocationID := "allocation-capability-concurrency"
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	baseTime := time.Now().UTC()
	observation := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt: timestamppb.New(baseTime), ValidUntil: timestamppb.New(baseTime.Add(capabilitycontract.HealthObservationValidity)),
		Evidence:   capabilitycontract.ConfigEvidence("sha256:" + strings.Repeat("a", 64)),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	snapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-capability-concurrency", Sequence: 1, SnapshotID: "snapshot-capability-concurrency",
		CollectedAt: timestamppb.New(baseTime), Observations: []*capabilityv1.CapabilityObservation{observation},
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key}, baseTime)
	if err != nil {
		t.Fatal(err)
	}
	initialConditions := healthyTestConditions(dependencies, baseTime)
	if _, err := controller.ReplaceCapabilityAdmission(allocationID, 1, testAllocationRequestDigest, dependencies, initialConditions, baseTime); err != nil {
		t.Fatal(err)
	}
	manifest := &apipb.AllocationEnforcementManifest{RuntimeName: "runsc", BundlePath: "/fake/" + allocationID, CreatedAtUnixNano: baseTime.UnixNano()}

	const updates = 50
	errs := make(chan error, updates*3)
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		for range updates {
			errs <- controller.StoreLaunchVerification(allocationID, manifest, nil, baseTime)
		}
	}()
	go func() {
		defer workers.Done()
		for generation := int64(1); generation <= updates; generation++ {
			errs <- controller.MergeCapabilityReconcile(allocationID, generation, []*capabilityv1.CapabilityKey{key})
		}
	}()
	go func() {
		defer workers.Done()
		for revision := 1; revision <= updates; revision++ {
			now := baseTime.Add(time.Duration(revision) * time.Millisecond)
			_, err := controller.UpdateCapabilityCondition(allocationID, &capabilityv1.CapabilityCondition{
				Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
				Message:    fmt.Sprintf("healthy-%d", revision), ObservedAt: timestamppb.New(now),
				Proof: proto.Clone(dependencies[0].GetSelectedObservation()).(*capabilityv1.CapabilityObservationProof),
			}, now)
			errs <- err
		}
	}()
	workers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	if got := controller.EnforcementManifest(allocationID); got == nil || got.GetBundlePath() != manifest.GetBundlePath() {
		t.Fatalf("enforcement manifest lost: %#v", got)
	}
	if got := controller.LaunchVerification(allocationID); got == nil || got.GetVerifiedAtUnixNano() != baseTime.UnixNano() {
		t.Fatalf("launch verification lost: %#v", got)
	}
	conditions := controller.CapabilityConditions(allocationID)
	if conditions == nil || conditions.GetRevision() != updates+1 || len(conditions.GetConditions()) != 1 {
		t.Fatalf("condition set lost updates: %#v", conditions)
	}
	reconcile := controller.CapabilityReconcileState(allocationID)
	if reconcile == nil || len(reconcile.GetPending()) != 1 || reconcile.GetPending()[0].GetGeneration() != updates {
		t.Fatalf("reconcile state lost updates: %#v", reconcile)
	}
	if dependencies := controller.CapabilityDependencyManifests()[allocationID]; len(dependencies) != 1 {
		t.Fatalf("dependency manifest lost: %#v", dependencies)
	}
	var persisted apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.GetEnforcementManifest() == nil || persisted.GetCapabilityConditions().GetRevision() != updates+1 || len(persisted.GetCapabilityReconcile().GetPending()) != 1 {
		t.Fatalf("durable allocation state is incomplete: %#v", &persisted)
	}
}

func TestStoreLaunchVerificationRequiresExactManifestEnforcementKeys(t *testing.T) {
	controller := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runc": runtimetest.NewFakeRuntimeHandler()}).controller
	const allocationID = "allocation-launch-verification"
	if _, err := controller.ReplaceCapabilityAdmission(allocationID, 1, testAllocationRequestDigest, nil, nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	manifest := &apipb.AllocationEnforcementManifest{
		RuntimeName: "runc", EphemeralStorageLimitBytes: 64 << 20,
		FilestoreMountIdentity: "42:/dev/loop0:/var/lib/axnoded/filestore", RuncProjectID: 1234,
		BundlePath: "/var/lib/axnoded/root/containers/" + allocationID, CreatedAtUnixNano: time.Now().UnixNano(),
	}
	if err := controller.StoreLaunchVerification(allocationID, manifest, nil, time.Now().UTC()); err == nil {
		t.Fatal("StoreLaunchVerification() accepted a missing hard-enforcement proof")
	}
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT)
	verifiedAt := time.Now().UTC()
	if err := controller.StoreLaunchVerification(allocationID, manifest, []*capabilityv1.CapabilityKey{key}, verifiedAt); err != nil {
		t.Fatal(err)
	}
	if err := controller.StoreLaunchVerification(allocationID, manifest, []*capabilityv1.CapabilityKey{key, key}, verifiedAt); err == nil {
		t.Fatal("StoreLaunchVerification() accepted duplicate proof keys")
	}
}

func TestBeginCapabilityTerminationIsDurableAndAggregatesReasons(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	controller := fixture.controller
	const allocationID = "allocation-capability-termination"
	if _, err := controller.ReplaceCapabilityAdmission(allocationID, 1, testAllocationRequestDigest, nil, nil, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := controller.BeginCapabilityTermination(allocationID, errors.New("memory enforcement lost")); err != nil {
		t.Fatal(err)
	}
	if err := controller.BeginCapabilityTermination(allocationID, errors.New("ephemeral enforcement lost")); err != nil {
		t.Fatal(err)
	}
	state := controller.CapabilityReconcileState(allocationID)
	if state == nil || !state.GetTerminating() {
		t.Fatalf("termination state = %#v", state)
	}
	if !strings.Contains(state.GetLastError(), "memory enforcement lost") || !strings.Contains(state.GetLastError(), "ephemeral enforcement lost") {
		t.Fatalf("termination reasons = %q", state.GetLastError())
	}
	var persisted apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.GetCapabilityReconcile() == nil || !persisted.GetCapabilityReconcile().GetTerminating() {
		t.Fatalf("persisted termination state = %#v", persisted.GetCapabilityReconcile())
	}
}

func TestCapabilityConditionKeysMustExactlyMatchDependencies(t *testing.T) {
	port := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	network := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	dependencies := []*capabilityv1.CapabilityDependency{{Key: port}, {Key: network}}
	matching := []*capabilityv1.CapabilityCondition{{Key: network}, {Key: port}}
	if !capabilityConditionKeysEqualDependencies(dependencies, matching) {
		t.Fatal("matching dependency and condition sets were rejected")
	}
	if capabilityConditionKeysEqualDependencies(dependencies, matching[:1]) {
		t.Fatal("condition set missing a dependency was accepted")
	}
	if capabilityConditionKeysEqualDependencies(dependencies, []*capabilityv1.CapabilityCondition{{Key: port}, {Key: port}}) {
		t.Fatal("duplicate condition key was accepted")
	}
}

func TestReplaceCapabilityAdmissionAtomicallyAdvancesProofAndConditionGeneration(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	controller := fixture.controller
	const allocationID = "allocation-capability-admission"
	now := time.Now().UTC()
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	observation := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(capabilitycontract.HealthObservationValidity)),
		Evidence: capabilitycontract.ConfigEvidence("sha256:" + strings.Repeat("b", 64)), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	snapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-capability-admission", Sequence: 1, SnapshotID: "snapshot-capability-admission",
		CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{observation},
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	conditions := healthyTestConditions(dependencies, now)
	first, err := controller.ReplaceCapabilityAdmission(allocationID, 1, testAllocationRequestDigest, dependencies, conditions, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetRevision() != 1 {
		t.Fatalf("initial revision = %d, want 1", first.GetRevision())
	}
	manifest := &apipb.AllocationEnforcementManifest{
		RuntimeName: "runsc", BundlePath: "/var/lib/axnoded/containers/" + allocationID,
		CreatedAtUnixNano: now.UnixNano(),
	}
	if err := controller.StoreLaunchVerification(allocationID, manifest, nil, now); err != nil {
		t.Fatal(err)
	}
	second, err := controller.ReplaceCapabilityAdmission(allocationID, 1, testAllocationRequestDigest, dependencies, conditions, now.Add(time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if second.GetRevision() != 2 {
		t.Fatalf("post-create revision = %d, want 2", second.GetRevision())
	}
	if _, err := controller.ReplaceCapabilityAdmission(allocationID, 2, testAllocationRequestDigest, dependencies, conditions, now.Add(2*time.Millisecond)); err == nil {
		t.Fatal("ReplaceCapabilityAdmission() accepted a conflicting allocation attempt")
	}
	conflictingDigest := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := controller.ReplaceCapabilityAdmission(allocationID, 1, conflictingDigest, dependencies, conditions, now.Add(2*time.Millisecond)); err == nil {
		t.Fatal("ReplaceCapabilityAdmission() accepted a conflicting request digest")
	}
	var persisted apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.GetAllocationAttempt() != 1 || persisted.GetAllocationRequestDigest() != testAllocationRequestDigest || persisted.GetCapabilityConditions().GetRevision() != 2 || persisted.GetCapabilityAdmissionConditions().GetRevision() != 2 || len(persisted.GetCapabilityDependencies()) != 1 {
		t.Fatalf("persisted admission is not atomic: %#v", &persisted)
	}
}

func TestNodeLocalCapabilityAdmissionPersistsWithoutControlPlaneAttempt(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()}, store, fakeVolumePublisher{})
	controller := fixture.controller
	const allocationID = "node-local-capability-admission"
	now := time.Now().UTC()
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	observation := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt: timestamppb.New(now), ValidUntil: timestamppb.New(now.Add(capabilitycontract.HealthObservationValidity)),
		Evidence: capabilitycontract.ConfigEvidence("sha256:" + strings.Repeat("c", 64)), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	snapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-node-local", Sequence: 1, SnapshotID: "snapshot-node-local",
		CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{observation},
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	conditions := healthyTestConditions(dependencies, now)
	set, err := controller.ReplaceNodeLocalCapabilityAdmission(allocationID, testAllocationRequestDigest, dependencies, conditions, now)
	if err != nil {
		t.Fatal(err)
	}
	if set.GetRevision() != 1 {
		t.Fatalf("condition revision = %d, want 1", set.GetRevision())
	}
	if attempt, managed := controller.ManagedAllocationAttempt(allocationID); attempt != 0 || managed {
		t.Fatalf("managed attempt = %d/%t, want 0/false", attempt, managed)
	}
	if len(controller.CapabilityDependencyManifests()[allocationID]) != 1 {
		t.Fatal("node-local dependency was not included in runtime reconciliation inventory")
	}
	var persisted apipb.AllocationState
	if err := store.GetRecord(config.AllocationStateBucket, allocationID, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.GetAllocationAttempt() != 0 || persisted.GetAllocationRequestDigest() != testAllocationRequestDigest || len(persisted.GetCapabilityDependencies()) != 1 {
		t.Fatalf("persisted node-local admission = %#v", &persisted)
	}
	if _, err := controller.ReplaceCapabilityAdmission(allocationID, 1, testAllocationRequestDigest, dependencies, conditions, now.Add(time.Millisecond)); err == nil {
		t.Fatal("managed admission took ownership of a node-local sandbox")
	}
}

func healthyTestConditions(dependencies []*capabilityv1.CapabilityDependency, observedAt time.Time) []*capabilityv1.CapabilityCondition {
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(dependencies))
	for _, dependency := range dependencies {
		conditions = append(conditions, &capabilityv1.CapabilityCondition{
			Key:        capabilitycontract.CloneKey(dependency.GetKey()),
			State:      capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			Message:    "available", ObservedAt: timestamppb.New(observedAt),
			Proof: proto.Clone(dependency.GetSelectedObservation()).(*capabilityv1.CapabilityObservationProof),
		})
	}
	return conditions
}
