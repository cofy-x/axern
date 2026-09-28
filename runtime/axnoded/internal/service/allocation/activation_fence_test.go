package allocation

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const activationFenceDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCapabilityReconcileDoesNotPersistWorkBeforeLaunchVerification(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, &runtimeSpyHandler{name: "runsc"}, store)
	const allocationID = "alloc-preactivation-reconcile"
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT)
	requirements := []*capabilityv1.CapabilityRequirement{{Key: key, LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP}}
	require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, requirements, nil, nil))
	var before apipb.AllocationState
	require.NoError(t, store.GetRecord(config.AllocationStateBucket, allocationID, &before))

	require.NoError(t, fixture.controller.MergeCapabilityReconcile(allocationID))
	require.Nil(t, fixture.controller.CapabilityReconcileState(allocationID))
	require.NotContains(t, fixture.controller.CapabilityRequirementManifests(), allocationID)
	var after apipb.AllocationState
	require.NoError(t, store.GetRecord(config.AllocationStateBucket, allocationID, &after))
	require.True(t, proto.Equal(&before, &after), "preactivation enqueue changed the durable create intent")
	recovery, err := fixture.controller.InspectRecoveryRecords()
	require.NoError(t, err)
	require.Contains(t, recovery.Intents, allocationID)
	require.NotContains(t, recovery.EnforcementVerified, allocationID)

	// The existing launch-verification barrier, not a second lifecycle flag,
	// makes this Allocation eligible for durable runtime-audit work.
	now := time.Now().UTC()
	require.NoError(t, fixture.controller.StoreVerifiedEnforcementManifest(allocationID, &apipb.AllocationEnforcementManifest{
		BundlePath: filepath.Join(fixture.controller.config.RootDir, "containers", allocationID), CreatedAtUnixNano: now.UnixNano(),
	}, []*capabilityv1.CapabilityKey{key}, now))
	require.NoError(t, fixture.controller.MergeCapabilityReconcile(allocationID))
	require.Equal(t, int64(1), fixture.controller.CapabilityReconcileState(allocationID).GetPendingIntentSequence())
	require.Contains(t, fixture.controller.CapabilityRequirementManifests(), allocationID)
	recovery, err = fixture.controller.InspectRecoveryRecords()
	require.NoError(t, err)
	require.Contains(t, recovery.EnforcementVerified, allocationID)
}

func TestStoreVerifiedEnforcementManifestRejectsTerminationIntent(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, &runtimeSpyHandler{name: "runsc"}, store)
	const allocationID = "alloc-terminated-before-verification"
	require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, nil, nil, nil))
	require.NoError(t, fixture.controller.MarkTerminationIntent(allocationID, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, "execution authority expired"))
	now := time.Now().UTC()
	err := fixture.controller.StoreVerifiedEnforcementManifest(allocationID, &apipb.AllocationEnforcementManifest{
		BundlePath: filepath.Join(fixture.controller.config.RootDir, "containers", allocationID), CreatedAtUnixNano: now.UnixNano(),
	}, nil, now)
	require.Error(t, err)
	require.Nil(t, fixture.controller.VerifiedEnforcementManifest(allocationID))
	var persisted apipb.AllocationState
	require.NoError(t, store.GetRecord(config.AllocationStateBucket, allocationID, &persisted))
	require.Nil(t, persisted.GetEnforcementManifest())
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, persisted.GetTerminationDiagnosticCode())
}

func TestStartAllocationRejectsTerminationIntentBeforeSideEffects(t *testing.T) {
	for _, lifecycleHeld := range []bool{false, true} {
		name := "ordinary-start"
		if lifecycleHeld {
			name = "lifecycle-held-start"
		}
		t.Run(name, func(t *testing.T) {
			handler := &runtimeSpyHandler{name: "runsc"}
			fixture := newTestAllocationController(t, handler)
			const allocationID = "alloc-terminated-before-create"
			require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, nil, nil, nil))
			require.NoError(t, fixture.controller.MarkTerminationIntent(allocationID, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, "execution authority expired"))
			request := &apipb.StartRequest{AllocationID: allocationID, Environment: testResolvedEnvironment(t, "environment-terminated-before-create")}
			var err error
			if lifecycleHeld {
				unlock := fixture.controller.LockAllocationLifecycle(allocationID)
				defer unlock()
				_, err = fixture.controller.StartWithLifecycleHeld(t.Context(), request)
			} else {
				_, err = fixture.controller.Start(t.Context(), request)
			}
			require.Error(t, err)
			require.Zero(t, handler.createCalls)
			require.Empty(t, fixture.environmentCache.List())
			code, message := fixture.controller.TerminationIntent(allocationID)
			require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, code)
			require.Equal(t, "execution authority expired", message)
		})
	}
}

func TestActiveAllocationReplayRejectsTerminationIntent(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)
	const allocationID = "alloc-terminated-replay"
	require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, nil, nil, nil))
	now := time.Now().UTC()
	require.NoError(t, fixture.controller.StoreVerifiedEnforcementManifest(allocationID, &apipb.AllocationEnforcementManifest{
		BundlePath: filepath.Join(fixture.controller.config.RootDir, "containers", allocationID), CreatedAtUnixNano: now.UnixNano(),
	}, nil, now))
	require.NoError(t, fixture.manager.StoreMetadata(allocationID, &apipb.ContainerMetadata{}))
	target, err := fixture.manager.Get(allocationID)
	require.NoError(t, err)
	require.NoError(t, target.Status.UpdateSync(func(current container.Status) (container.Status, error) {
		current.RuntimeState = apipb.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_RUNNING
		return current, nil
	}))
	require.NoError(t, fixture.controller.MarkTerminationIntent(allocationID, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST, "allocation capability enforcement was lost"))
	request := &apipb.StartRequest{AllocationID: allocationID, Environment: testResolvedEnvironment(t, "environment-terminated-replay")}
	unlock := fixture.controller.LockAllocationLifecycle(allocationID)
	defer unlock()
	_, _, err = fixture.controller.ExistingActiveStartResponseWithLifecycleHeld(t.Context(), request)
	require.Error(t, err, "a runtime projection cannot reactivate a terminated Allocation")
	_, err = fixture.controller.StartWithLifecycleHeld(t.Context(), request)
	require.Error(t, err)
	require.Zero(t, handler.createCalls)
}

type observedWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *observedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestAllocationLifecycleLockWaitIsContextCancelable(t *testing.T) {
	fixture := newTestAllocationController(t, &runtimeSpyHandler{name: "runsc"})
	const allocationID = "alloc-cancel-lifecycle-wait"
	unlock := fixture.controller.LockAllocationLifecycle(allocationID)
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queued := &observedWaitContext{Context: ctx, waiting: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		release, err := fixture.controller.LockAllocationLifecycleContext(queued, allocationID)
		if err == nil {
			release()
		}
		done <- err
	}()
	// Done is evaluated by the lane's wait select, after its initial Err check
	// and registration. An entrance-only cancellation check cannot pass this.
	<-queued.waiting
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		// This timeout bounds a deadlock; ordering is established by the held
		// lifecycle gate and explicit cancellation, never machine speed.
		t.Fatal("cancelled lifecycle waiter did not return while Start still owned the gate")
	}
}

type activationFenceRuntime struct {
	*runtimeSpyHandler
	activations int
}

func (h *activationFenceRuntime) StartPreparedContainer(ctx context.Context, prepared *contract.PreparedContainer, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	h.activations++
	return h.runtimeSpyHandler.StartPreparedContainer(ctx, prepared, options)
}

func TestAllocationActivationRejectsTerminationPersistedInsideGate(t *testing.T) {
	handler := &activationFenceRuntime{runtimeSpyHandler: &runtimeSpyHandler{name: "runsc"}}
	fixture := newTestAllocationController(t, handler)
	const allocationID = "alloc-terminated-before-activation"
	require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, nil, nil, nil))
	fixture.controller.preActivationCapabilityGate = func(_ context.Context, _ *apipb.StartRequest, _ contract.AllocationRuntime, id string) error {
		return fixture.controller.MarkTerminationIntentWithLifecycleHeld(id, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, "execution authority expired")
	}
	_, err := fixture.controller.Start(t.Context(), &apipb.StartRequest{AllocationID: allocationID, Environment: testResolvedEnvironment(t, "environment-terminated-at-activation")})
	require.Error(t, err)
	require.Equal(t, 1, handler.createCalls, "test did not reach the real preactivation gate")
	require.Zero(t, handler.activations, "termination was ignored between preparation and activation")
}

func TestExpirySelectionRechecksRenewalBeforeRecordingTermination(t *testing.T) {
	fixture := newTestAllocationController(t, &runtimeSpyHandler{name: "runsc"})
	const id = "alloc-renewed-behind-start"
	now := time.Now().UTC()
	require.NoError(t, fixture.controller.StoreAllocationIntent(id, "node-a", activationFenceDigest, now.Add(time.Minute), nil, nil, nil, nil))
	selectedAt := now.Add(2 * time.Minute)
	require.Contains(t, fixture.controller.ExpiredExecutionLeaseAllocationIDs(selectedAt), id)
	// An already received grant wins the record transaction before the
	// watchdog acquires the lifecycle lane. Its earlier selection is stale.
	require.NoError(t, fixture.controller.RenewExecutionLeases(map[string]time.Duration{id: 3 * time.Minute}, now.Add(30*time.Second)))
	unlock := fixture.controller.LockAllocationLifecycle(id)
	defer unlock()
	expired, err := fixture.controller.MarkExpiredExecutionLeaseWithLifecycleHeld(id, selectedAt)
	require.NoError(t, err)
	require.False(t, expired)
	code, _ := fixture.controller.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED, code)
	expired, err = fixture.controller.MarkExpiredExecutionLeaseWithLifecycleHeld(id, now.Add(4*time.Minute))
	require.NoError(t, err)
	require.True(t, expired)
	code, _ = fixture.controller.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, code)
}
