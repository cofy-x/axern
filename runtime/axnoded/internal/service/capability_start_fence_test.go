package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/require"
)

type capabilityStartBarrierRuntime struct {
	*runtimeSpyHandler
	prepareEntered chan struct{}
	prepareRelease chan struct{}
	lost           atomic.Bool
	verifyHook     func() contract.CapabilityVerification
}

func (r *capabilityStartBarrierRuntime) PrepareContainer(ctx context.Context, request *apipb.CreateContainerRequest, options contract.HandlerOptions) (*contract.PreparedContainer, error) {
	close(r.prepareEntered)
	select {
	case <-r.prepareRelease:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.runtimeSpyHandler.PrepareContainer(ctx, request, options)
}

func (r *capabilityStartBarrierRuntime) AllocationEnforcementManifest(_ context.Context, id string) (*apipb.AllocationEnforcementManifest, error) {
	return &apipb.AllocationEnforcementManifest{
		BundlePath: "/fake/" + id, CreatedAtUnixNano: time.Now().UnixNano(),
		EphemeralStorageLimitBytes: 64 << 20, RunscBackingDirectory: "/fake/filestore/runsc",
		RunscBackingDirectoryIdentity: "devino:v1:1:2", FilestoreMountIdentity: "42:/dev/loop0:/fake/filestore",
		RunscOverlayArg: "root:dir=/fake/filestore/runsc,size=67108864",
	}, nil
}

func (r *capabilityStartBarrierRuntime) VerifyAllocationCapability(context.Context, *capabilityv1.CapabilityRequirement, contract.HandlerOptions) contract.CapabilityVerification {
	if r.verifyHook != nil {
		return r.verifyHook()
	}
	if r.lost.Load() {
		return contract.LostCapability(errors.New("private verifier detail: destination.example:443 /private/runtime"))
	}
	return contract.VerifiedCapability()
}

func (r *capabilityStartBarrierRuntime) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return []*contract.UnionContainerState{{ID: r.lastOptions.ContainerID, Status: contract.ContainerStatusRunning, InitProcessPid: 101, Created: time.Now().UTC().Format(time.RFC3339Nano)}}, nil
}

func ephemeralCapabilityManager(t *testing.T) (*capabilitymanager.Manager, []*capabilityv1.CapabilityRequirement) {
	t.Helper()
	overlay := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTest := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	hardLimit := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	manager, err := capabilitymanager.NewManager(
		observedProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, expected: []*capabilityv1.CapabilityKey{overlay},
			observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
				return []*capabilityv1.CapabilityObservation{availableObservation(overlay, capabilitycontract.MountEvidence(testCapabilityBootID, "42:/dev/loop0:/fake/filestore"))}, nil
			}},
		observedProvider{provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, expected: []*capabilityv1.CapabilityKey{selfTest},
			observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
				return []*capabilityv1.CapabilityObservation{availableObservation(selfTest, capabilitycontract.RuntimeEvidence(testCapabilityBootID, sha256Digest([]byte("runsc")), sha256Digest([]byte("config"))))}, nil
			}},
		derivedCapabilityProvider{expected: []*capabilityv1.CapabilityKey{hardLimit}},
	)
	require.NoError(t, err)
	now := time.Now().UTC()
	snapshot, err := manager.Refresh(t.Context(), now)
	require.NoError(t, err)
	dependencies, err := capabilitycontract.ResolveRequirements(snapshot, []*capabilityv1.CapabilityKey{hardLimit}, now)
	require.NoError(t, err)
	return manager, dependencies
}

// This is the platform reducer for #190: freeze runtime preparation after the
// admission intent is visible, then enqueue the exact background operation.
// Correctness is established by the barriers and durable state, not timing.
func TestCapabilityReconcileDoesNotAuditPreparingAllocation(t *testing.T) {
	handler := &capabilityStartBarrierRuntime{
		runtimeSpyHandler: &runtimeSpyHandler{name: "runsc", waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
			<-ctx.Done()
			return contract.Exit{}, ctx.Err()
		}},
		prepareEntered: make(chan struct{}), prepareRelease: make(chan struct{}),
	}
	service := newTestService(t, handler)
	manager, dependencies := ephemeralCapabilityManager(t)
	service.capabilityManager = manager
	service.capabilityReconcileCtx = t.Context()
	const id = "allocation-preactivation-reconcile"
	request := &apipb.StartRequest{
		AllocationID: id,
		Environment: &apipb.ResolvedEnvironment{ID: "environment-start-fence", Argv: []string{"/bin/true"},
			Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}}},
		Network:                &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_HOST},
		Resources:              &commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{EphemeralStorageBytes: 64 << 20}},
		CapabilityRequirements: dependencies,
	}
	started := make(chan error, 1)
	startFinished := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(handler.prepareRelease) }) }
	t.Cleanup(func() { release(); <-startFinished })
	go func() { defer close(startFinished); _, err := service.Start(t.Context(), request); started <- err }()
	select {
	case <-handler.prepareEntered:
	case err := <-started:
		t.Fatalf("Start did not enter runtime preparation: %v", err)
	case <-t.Context().Done():
		t.Fatal("test cancelled before preparation")
	}
	require.NotEmpty(t, service.allocations.CapabilityRequirements(id), "the immutable intent must already be visible")
	require.Nil(t, service.allocations.VerifiedEnforcementManifest(id))
	require.NoError(t, service.allocations.MergeCapabilityReconcile(id))
	require.Nil(t, service.allocations.CapabilityReconcileState(id), "preactivation intent became runtime audit work")
	require.NotContains(t, service.allocations.CapabilityRequirementManifests(), id)
	code, _ := service.allocations.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED, code)
	release()
	require.NoError(t, <-started)
	require.NotNil(t, service.allocations.VerifiedEnforcementManifest(id))
	require.Contains(t, service.allocations.CapabilityRequirementManifests(), id)
	_, conditions, err := service.ReconcileAllocationCapabilities(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY, conditions.GetConditions()[0].GetState())

	// The same execution still fails closed on a real post-activation loss;
	// the final diagnosis survives acknowledgement without exposing raw detail.
	handler.lost.Store(true)
	_, conditions, err = service.ReconcileAllocationCapabilities(t.Context(), id)
	require.NoError(t, err)
	service.capabilityReconcileWG.Wait()
	require.Equal(t, capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED, conditions.GetConditions()[0].GetState())
	code, diagnosis := service.allocations.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST, code)
	require.Contains(t, diagnosis, "runsc_ephemeral_storage_hard_limit")
	require.NotContains(t, diagnosis, "destination.example")
	require.NotContains(t, diagnosis, "/private/runtime")
	require.NotContains(t, conditions.GetConditions()[0].GetMessage(), "destination.example")
	require.Equal(t, 1, handler.stopCalls)
	require.Zero(t, handler.deleteCalls, "fail-stop must preserve the output sealing barrier")
	require.True(t, service.allocations.HasAllocation(id))
}

func TestCapabilityFailStopCannotAcknowledgeBehindActivationFence(t *testing.T) {
	service := newTestService(t, &runtimeSpyHandler{name: "runsc"})
	const id = "allocation-fail-stop-activation-fence"
	now := time.Now().UTC()
	require.NoError(t, service.allocations.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now.Add(time.Minute), nil, nil, nil, nil))
	require.NoError(t, service.allocations.StoreVerifiedEnforcementManifest(id, &apipb.AllocationEnforcementManifest{BundlePath: "/fake/" + id, CreatedAtUnixNano: now.UnixNano()}, nil, now))
	unlock := service.allocations.LockAllocationLifecycle(id)
	require.NoError(t, service.allocations.BeginCapabilityTerminationWithLifecycleHeld(id, errors.New("test enforcement loss")))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Cancellation bounds a worker behind the lane still owned by Start.
	// Absent metadata cannot bypass that lane and acknowledge termination.
	service.failStopAllocation(ctx, id, errors.New("test enforcement loss"))
	require.True(t, service.allocations.CapabilityReconcileState(id).GetTerminating())
	unlock()
	service.failStopAllocation(t.Context(), id, errors.New("test enforcement loss"))
	require.Nil(t, service.allocations.CapabilityReconcileState(id))
	require.True(t, service.allocations.HasAllocation(id))
}

type capabilityAckWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *capabilityAckWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestCapabilityEvaluationAckCannotClearIndependentTermination(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	service := newTestService(t, handler)
	service.capabilityReconcileCtx = t.Context()
	const id = "allocation-evaluation-ack-termination-fence"
	now := time.Now().UTC()
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT)
	requirements := []*capabilityv1.CapabilityRequirement{{Key: key, LossPolicy: capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP}}
	require.NoError(t, service.allocations.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now.Add(time.Minute), nil, requirements, nil, nil))
	require.NoError(t, service.allocations.StoreVerifiedEnforcementManifest(id, &apipb.AllocationEnforcementManifest{BundlePath: "/fake/" + id, CreatedAtUnixNano: now.UnixNano()}, []*capabilityv1.CapabilityKey{key}, now))
	require.NoError(t, service.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{}))
	markTestContainerRunning(t, service, id)
	require.NoError(t, service.allocations.MergeCapabilityReconcile(id))
	sequence := service.allocations.CapabilityReconcileState(id).GetPendingIntentSequence()

	unlock := service.allocations.LockAllocationLifecycle(id)
	var unlockOnce sync.Once
	release := func() { unlockOnce.Do(unlock) }
	ctx, cancel := context.WithCancel(t.Context())
	queued := &capabilityAckWaitContext{Context: ctx, waiting: make(chan struct{})}
	ack := make(chan error, 1)
	ackFinished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		release()
		<-ackFinished
	})
	go func() {
		defer close(ackFinished)
		ack <- service.ackCapabilityEvaluation(queued, id, sequence)
	}()
	// The old healthy audit's acknowledgement has entered its lane wait. An
	// independent lifecycle query commits enforcement loss before that wait is
	// released; the ordering is explicit, not inferred from a sleep.
	select {
	case <-queued.waiting:
	case err := <-ack:
		t.Fatalf("evaluation acknowledgement bypassed the lifecycle lane: %v", err)
	case <-t.Context().Done():
		t.Fatal("test cancelled before evaluation acknowledgement queued")
	}
	require.NoError(t, service.allocations.BeginCapabilityTerminationWithLifecycleHeld(id, errors.New("independent lifecycle query detected enforcement loss")))
	termination := service.allocations.CapabilityReconcileState(id)
	require.True(t, termination.GetTerminating())
	release()
	require.NoError(t, <-ack)
	require.Equal(t, termination, service.allocations.CapabilityReconcileState(id), "old evaluation erased independently committed fail-stop work")

	// Consume the surviving intent through the real worker. It must stop only
	// the supervised workload and retain the authoritative final Delete barrier.
	service.startCapabilityReconcileWorker(id)
	service.capabilityReconcileWG.Wait()
	require.Equal(t, 1, handler.stopCalls)
	require.Zero(t, handler.deleteCalls)
	require.True(t, service.allocations.HasAllocation(id))
	_, err := service.containerManager.Get(id)
	require.NoError(t, err, "fail-stop deleted runtime metadata before output sealing")
	require.Nil(t, service.allocations.CapabilityReconcileState(id))
	code, diagnosis := service.allocations.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST, code)
	require.Contains(t, diagnosis, "independent lifecycle query detected enforcement loss")
}

func TestCapabilityEvaluationAckCancellationPreservesIntent(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	service := newTestService(t, handler)
	const id = "allocation-cancel-evaluation-ack"
	now := time.Now().UTC()
	require.NoError(t, service.allocations.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now.Add(time.Minute), nil, nil, nil, nil))
	require.NoError(t, service.allocations.StoreVerifiedEnforcementManifest(id, &apipb.AllocationEnforcementManifest{BundlePath: "/fake/" + id, CreatedAtUnixNano: now.UnixNano()}, nil, now))
	require.NoError(t, service.allocations.MergeCapabilityReconcile(id))
	pending := service.allocations.CapabilityReconcileState(id)
	unlock := service.allocations.LockAllocationLifecycle(id)
	ctx, cancel := context.WithCancel(t.Context())
	queued := &capabilityAckWaitContext{Context: ctx, waiting: make(chan struct{})}
	ack := make(chan error, 1)
	ackFinished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		unlock()
		<-ackFinished
	})
	go func() {
		defer close(ackFinished)
		ack <- service.ackCapabilityEvaluation(queued, id, pending.GetPendingIntentSequence())
	}()
	select {
	case <-queued.waiting:
	case err := <-ack:
		t.Fatalf("evaluation acknowledgement bypassed the lifecycle lane: %v", err)
	case <-t.Context().Done():
		t.Fatal("test cancelled before evaluation acknowledgement queued")
	}
	cancel()
	require.ErrorIs(t, <-ack, context.Canceled, "worker shutdown must leave the still-owned lifecycle lane")
	require.Equal(t, pending, service.allocations.CapabilityReconcileState(id), "cancelled evaluation acknowledged durable retry work")
	require.Zero(t, handler.stopCalls)
	require.Zero(t, handler.deleteCalls)
}

type capabilityFailStopInventoryRuntime struct {
	*runtimeSpyHandler
	inventory      []*contract.UnionContainerState
	inventoryErr   error
	inventoryHook  func()
	inventoryCalls int
}

func (r *capabilityFailStopInventoryRuntime) ListContainers(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	r.inventoryCalls++
	if r.inventoryHook != nil {
		r.inventoryHook()
	}
	return r.inventory, r.inventoryErr
}

func TestCapabilityFailStopRetainsIntentWithoutVerifiedRuntimeAbsence(t *testing.T) {
	const id = "allocation-unconfirmed-runtime-absence"
	inventoryErr := errors.New("runtime inventory unavailable")
	for _, test := range []struct {
		name         string
		inventory    []*contract.UnionContainerState
		inventoryErr error
		wantErr      error
	}{
		{
			name: "runtime-present-without-metadata",
			inventory: []*contract.UnionContainerState{{
				ID: id, Status: contract.ContainerStatusRunning,
			}},
			wantErr: errord.ErrFailedPrecondition,
		},
		{
			name: "runtime-inventory-unavailable", inventoryErr: inventoryErr, wantErr: inventoryErr,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &capabilityFailStopInventoryRuntime{
				runtimeSpyHandler: &runtimeSpyHandler{name: "runsc"},
				inventory:         test.inventory, inventoryErr: test.inventoryErr,
			}
			service := newTestService(t, handler)
			now := time.Now().UTC()
			require.NoError(t, service.allocations.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now.Add(time.Minute), nil, nil, nil, nil))
			require.NoError(t, service.allocations.StoreVerifiedEnforcementManifest(id, &apipb.AllocationEnforcementManifest{BundlePath: "/fake/" + id, CreatedAtUnixNano: now.UnixNano()}, nil, now))
			require.NoError(t, service.allocations.BeginCapabilityTermination(id, errors.New("test enforcement loss")))
			_, err := service.containerManager.Get(id)
			require.ErrorIs(t, err, errord.ErrNotFound)
			before := service.allocations.CapabilityReconcileState(id)
			require.True(t, before.GetTerminating())

			err = service.allocations.FailStopWorkload(t.Context(), id)
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, before, service.allocations.CapabilityReconcileState(id))

			// Cancel only once the worker has actually read the inventory. This
			// bounds its retry loop without skipping the failed stop/ack boundary
			// and without relying on a sleep or a scheduling delay.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			handler.inventoryHook = cancel
			service.failStopAllocation(ctx, id, errors.New("test enforcement loss"))
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, 2, handler.inventoryCalls)
			require.Equal(t, before, service.allocations.CapabilityReconcileState(id), "unconfirmed absence acknowledged durable fail-stop work")
			code, _ := service.allocations.TerminationIntent(id)
			require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST, code)
			require.True(t, service.allocations.HasAllocation(id))
			require.Zero(t, handler.stopCalls)
			require.Zero(t, handler.deleteCalls)
		})
	}
}

func TestCapabilityReconcileRejectsRuntimeWithoutLaunchVerification(t *testing.T) {
	service := newTestService(t, &runtimeSpyHandler{name: "runsc"})
	const id = "allocation-missing-launch-verification"
	require.NoError(t, service.allocations.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Now().Add(time.Minute), nil, nil, nil, nil))
	require.NoError(t, service.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{}))
	markTestContainerRunning(t, service, id)
	_, set, err := service.ReconcileAllocationCapabilities(t.Context(), id)
	require.ErrorContains(t, err, "without launch verification")
	require.Nil(t, set, "an identity conflict cannot publish healthy conditions")
	require.True(t, service.allocations.HasAllocation(id))
}

func TestCapabilityReconcileDoesNotFailStopWorkloadCompletedDuringVerification(t *testing.T) {
	handler := &capabilityStartBarrierRuntime{runtimeSpyHandler: &runtimeSpyHandler{name: "runsc"}}
	service := newTestService(t, handler)
	manager, dependencies := ephemeralCapabilityManager(t)
	service.capabilityManager = manager
	const id = "allocation-completed-during-verification"
	now := time.Now().UTC()
	require.NoError(t, service.allocations.StoreAllocationIntent(id, "node-a", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", now.Add(time.Minute), nil, dependencies, nil, nil))
	manifest, err := handler.AllocationEnforcementManifest(t.Context(), id)
	require.NoError(t, err)
	require.NoError(t, service.allocations.StoreVerifiedEnforcementManifest(id, manifest, []*capabilityv1.CapabilityKey{dependencies[0].GetKey()}, now))
	require.NoError(t, service.containerManager.StoreMetadata(id, &apipb.ContainerMetadata{}))
	markTestContainerRunning(t, service, id)
	handler.verifyHook = func() contract.CapabilityVerification {
		zero := int32(0)
		require.NoError(t, service.containerManager.SetExit(id, &zero, now, "", commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED))
		return contract.LostCapability(errors.New("workload already completed"))
	}
	_, set, err := service.ReconcileAllocationCapabilities(t.Context(), id)
	require.NoError(t, err)
	require.Nil(t, set)
	code, _ := service.allocations.TerminationIntent(id)
	require.Equal(t, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED, code)
	require.Nil(t, service.allocations.CapabilityReconcileState(id))
	require.Zero(t, handler.stopCalls)
}
