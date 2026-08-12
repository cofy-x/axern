package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestCapabilityVerificationRetriesOnlyInconclusiveResults(t *testing.T) {
	tests := []struct {
		name      string
		results   []contract.CapabilityVerification
		wantCalls int
		wantState contract.CapabilityVerificationState
	}{
		{name: "verified immediately", results: []contract.CapabilityVerification{contract.VerifiedCapability()}, wantCalls: 1, wantState: contract.CapabilityVerificationVerified},
		{name: "definitive loss immediately", results: []contract.CapabilityVerification{contract.LostCapability(errors.New("lost"))}, wantCalls: 1, wantState: contract.CapabilityVerificationLost},
		{name: "inconclusive then verified", results: []contract.CapabilityVerification{contract.InconclusiveCapability(errors.New("read")), contract.VerifiedCapability()}, wantCalls: 2, wantState: contract.CapabilityVerificationVerified},
		{name: "three inconclusive results", results: []contract.CapabilityVerification{contract.InconclusiveCapability(errors.New("one")), contract.InconclusiveCapability(errors.New("two")), contract.InconclusiveCapability(errors.New("three"))}, wantCalls: 3, wantState: contract.CapabilityVerificationInconclusive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			got, err := verifyCapabilityWithDelays(context.Background(), []time.Duration{0, 0, 0}, func() contract.CapabilityVerification {
				result := tt.results[calls]
				calls++
				return result
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls || got.State != tt.wantState {
				t.Fatalf("calls=%d state=%d, want calls=%d state=%d", calls, got.State, tt.wantCalls, tt.wantState)
			}
		})
	}
}

func TestCapabilityVerificationStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := verifyCapabilityWithDelays(ctx, []time.Duration{0, time.Hour}, func() contract.CapabilityVerification {
		calls++
		cancel()
		return contract.InconclusiveCapability(errors.New("read"))
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v calls=%d, want canceled after one call", err, calls)
	}
}

func TestCapabilityVerificationBatchDoesNotDelayDefinitiveLoss(t *testing.T) {
	calls := []int{0, 0}
	results, err := verifyCapabilityBatchWithDelays(context.Background(), []time.Duration{0, 0, 0}, 2, func(index int) contract.CapabilityVerification {
		calls[index]++
		if index == 1 {
			return contract.LostCapability(errors.New("hard limit lost"))
		}
		return contract.InconclusiveCapability(errors.New("temporarily unreadable"))
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls[0] != 1 || calls[1] != 1 {
		t.Fatalf("verification calls = %v, definitive loss waited for unrelated retries", calls)
	}
	if results[0].State != contract.CapabilityVerificationInconclusive || results[1].State != contract.CapabilityVerificationLost {
		t.Fatalf("verification results = %#v", results)
	}
}

func TestCapabilityVerificationRejectsInvalidRetrySchedule(t *testing.T) {
	if _, err := verifyCapabilityBatchWithDelays(context.Background(), []time.Duration{time.Second, 0}, 1, func(int) contract.CapabilityVerification {
		return contract.VerifiedCapability()
	}); err == nil {
		t.Fatal("verifyCapabilityBatchWithDelays() accepted a decreasing retry schedule")
	}
}

func TestCapabilityReconcileInterruptionRequestsRetryInsteadOfFailStop(t *testing.T) {
	service := newTestService(t, map[string]contract.RuntimeHandler{"runc": runtimetest.NewFakeRuntimeHandler()})
	dependency := &capabilityv1.CapabilityDependency{
		Key:        capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT),
		LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.verifyFailStopCapability(ctx, "missing-allocation", dependency)
	if err == nil {
		t.Fatal("interrupted capability verification did not request retry")
	}
}

func TestCapabilityConditionPersistenceFailureIsReturned(t *testing.T) {
	service := newTestService(t, map[string]contract.RuntimeHandler{"runsc": runtimetest.NewFakeRuntimeHandler()})
	dependency := &capabilityv1.CapabilityDependency{
		Key:        capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING),
		LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE,
	}
	err := service.reportCapabilityCondition(
		"missing-allocation", dependency,
		capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED,
		capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_ENFORCEMENT_LOST,
		"dataplane unavailable",
	)
	if err == nil {
		t.Fatal("condition persistence failure was discarded")
	}
}

func TestPeriodicCapabilityAuditCoversOperationalAndFailStopDependencies(t *testing.T) {
	port := &capabilityv1.CapabilityDependency{
		Key:        capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING),
		LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE,
	}
	memory := &capabilityv1.CapabilityDependency{
		Key:        capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT),
		LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP,
	}
	erofs := &capabilityv1.CapabilityDependency{
		Key:        capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS),
		LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY,
	}
	keys := periodicCapabilityAuditKeys([]*capabilityv1.CapabilityDependency{port, memory, erofs})
	if len(keys) != 2 {
		t.Fatalf("periodic audit keys = %d, want operational and fail-stop only", len(keys))
	}
	if !capabilitycontract.RequirementKeysEqual(keys, []*capabilityv1.CapabilityKey{port.GetKey(), memory.GetKey()}) {
		t.Fatalf("periodic audit keys = %#v", keys)
	}
}

func TestCapabilityAuditShardsAreStableAndCoverOneCycle(t *testing.T) {
	const allocationID = "allocation-stable-shard"
	want := capabilityAuditShard(allocationID)
	if want >= capabilityAuditShardCount || capabilityAuditShard(allocationID) != want {
		t.Fatalf("unstable capability audit shard %d", want)
	}
	current := capabilityAuditShardAt(time.Now().UTC())
	seen := make(map[uint32]struct{}, capabilityAuditShardCount)
	for index := 0; index < capabilityAuditShardCount; index++ {
		seen[current] = struct{}{}
		current = nextCapabilityAuditShard(current)
	}
	if len(seen) != capabilityAuditShardCount {
		t.Fatalf("audit cycle covered %d shards, want %d", len(seen), capabilityAuditShardCount)
	}
}

func TestCapabilityReconcileWorkerOwnershipIsPerAllocationAndBounded(t *testing.T) {
	service := &sandboxService{}
	for index := 0; index < capabilityReconcileWorkers; index++ {
		if !service.acquireCapabilityReconcileWorker(fmt.Sprintf("allocation-%d", index)) {
			t.Fatalf("worker %d was rejected below the node budget", index)
		}
	}
	if service.acquireCapabilityReconcileWorker("allocation-over-budget") {
		t.Fatal("worker started above the node-wide budget")
	}
	if service.acquireCapabilityReconcileWorker("allocation-0") {
		t.Fatal("second worker acquired the same allocation")
	}
	service.releaseCapabilityReconcileBudget("allocation-0")
	if !service.acquireCapabilityReconcileWorker("allocation-over-budget") {
		t.Fatal("terminating cleanup retained a verification permit")
	}
	if service.acquireCapabilityReconcileWorker("allocation-0") {
		t.Fatal("terminating cleanup lost its unique allocation ownership")
	}
	service.finishCapabilityReconcileWorker("allocation-0")
	service.finishCapabilityReconcileWorker("allocation-1")
	if !service.acquireCapabilityReconcileWorker("allocation-0") {
		t.Fatal("finished cleanup retained allocation ownership")
	}
}

func TestVerifiedAllocationEnforcementDoesNotRefreshExpiredNodeEvidence(t *testing.T) {
	now := time.Now().UTC()
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	observation := &capabilityv1.CapabilityObservation{
		Key:          key,
		State:        capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:     capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt:   timestamppb.New(now.Add(-21 * time.Minute)),
		ValidUntil:   timestamppb.New(now.Add(-time.Minute)),
		Evidence:     capabilitycontract.ConfigEvidence("sha256:" + strings.Repeat("a", 64)),
		ReasonCode:   capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		Dependencies: nil,
	}
	capabilitycontract.NormalizeObservation(observation)
	snapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-a",
		Sequence:       1,
		SnapshotID:     "snapshot-a",
		CollectedAt:    timestamppb.New(now),
		Observations:   []*capabilityv1.CapabilityObservation{observation},
	}

	state, reasonCode := verifiedCapabilityCondition(snapshot, key, now)
	if state != capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED || reasonCode != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE {
		t.Fatalf("state = %v, reason = %v", state, reasonCode)
	}
}

func TestPostCreateGateUsesDurablePreActivationProofAfterRuntimeExit(t *testing.T) {
	now := time.Now().UTC()
	overlay := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	quota := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA)
	selfTest := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	hardLimit := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT)
	mountEvidence := capabilitycontract.MountEvidence(testCapabilityBootID, "42:/dev/loop0:/var/lib/axnoded/filestore")
	filestore := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE,
		expected: []*capabilityv1.CapabilityKey{overlay, quota},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{availableObservation(overlay, mountEvidence), availableObservation(quota, mountEvidence)}, nil
		},
	}
	runtimeSelfTest := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST,
		expected: []*capabilityv1.CapabilityKey{selfTest},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			evidence := capabilitycontract.RuntimeEvidence(testCapabilityBootID, "runc", sha256Digest([]byte("runc")), sha256Digest([]byte("config")))
			return []*capabilityv1.CapabilityObservation{availableObservation(selfTest, evidence)}, nil
		},
	}
	manager, err := capabilitymanager.NewManager(filestore, runtimeSelfTest, derivedCapabilityProvider{expected: []*capabilityv1.CapabilityKey{hardLimit}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Refresh(context.Background(), now)
	if err != nil {
		t.Fatal(err)
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{hardLimit}, now)
	if err != nil {
		t.Fatal(err)
	}
	service := newTestService(t, map[string]contract.RuntimeHandler{"runc": runtimetest.NewFakeRuntimeHandler()})
	service.capabilityManager = manager
	const allocationID = "post-create-fast-exit"
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(dependencies))
	for _, dependency := range dependencies {
		conditions = append(conditions, &capabilityv1.CapabilityCondition{
			Key: capabilitycontract.CloneKey(dependency.GetKey()), State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE, Message: "available",
			ObservedAt: timestamppb.New(now), Proof: dependency.GetSelectedObservation(),
		})
	}
	if _, err := service.allocationController().ReplaceCapabilityAdmission(allocationID, 1, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", dependencies, conditions, now); err != nil {
		t.Fatal(err)
	}
	manifest := &apipb.AllocationEnforcementManifest{
		RuntimeName: "runc", EphemeralStorageLimitBytes: 64 << 20, RuncProjectID: 1234,
		FilestoreMountIdentity: "42:/dev/loop0:/var/lib/axnoded/filestore",
		BundlePath:             "/var/lib/axnoded/root/containers/" + allocationID, CreatedAtUnixNano: now.UnixNano(),
	}
	if err := service.allocationController().StoreLaunchVerification(allocationID, manifest, []*capabilityv1.CapabilityKey{hardLimit}, now); err != nil {
		t.Fatal(err)
	}
	admitted, conditions, err := service.verifyPostCreateCapabilityDependencies(context.Background(), allocationID, dependencies, now)
	if err != nil {
		t.Fatalf("post-create verification read mutable runtime state after exit: %v", err)
	}
	if len(admitted) != 1 || len(conditions) != 1 || !strings.Contains(conditions[0].GetMessage(), "before workload start") {
		t.Fatalf("post-create proof projection = dependencies:%#v conditions:%#v", admitted, conditions)
	}
}
