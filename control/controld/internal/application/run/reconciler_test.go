package apprun

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestReconcilerCompletesDeleteRetry(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{items: []allocationkernel.ReconcileItem{{
		AllocationID:   "alloc-a",
		LifecycleState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING,
		NodeID:         "node-a",
		NodeTarget:     "node-a:24010",
	}}}
	lifecycle := &fakeReconcileLifecycle{}

	if err := NewReconciler(store, lifecycle, "worker-a", func() time.Time { return now }).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if lifecycle.deleted != 1 {
		t.Fatalf("delete calls = %d, want 1", lifecycle.deleted)
	}
	if store.completedAllocationID != "alloc-a" {
		t.Fatalf("completed = %q, want alloc-a", store.completedAllocationID)
	}
	if store.scheduledAllocationID != "" {
		t.Fatalf("scheduled retry for successful delete: %q", store.scheduledAllocationID)
	}
}

func TestReconcilerReschedulesDeleteRetryFailure(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{items: []allocationkernel.ReconcileItem{{
		AllocationID:   "alloc-a",
		LifecycleState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING,
		NodeID:         "node-a",
		NodeTarget:     "node-a:24010",
	}}}
	lifecycle := &fakeReconcileLifecycle{deleteErr: errors.New("node unavailable")}

	if err := NewReconciler(store, lifecycle, "worker-a", func() time.Time { return now }).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if store.completedAllocationID != "" {
		t.Fatalf("completed allocation after failed delete: %q", store.completedAllocationID)
	}
	if store.scheduledAllocationID != "alloc-a" {
		t.Fatalf("scheduled allocation = %q, want alloc-a", store.scheduledAllocationID)
	}
	if store.scheduledIntent != allocationkernel.ReconcileIntentEnsureAbsent {
		t.Fatalf("scheduled intent = %q, want %q", store.scheduledIntent, allocationkernel.ReconcileIntentEnsureAbsent)
	}
	if want := now.Add(allocationkernel.DeleteRetryDelay); !store.scheduledNextRunAt.Equal(want) {
		t.Fatalf("scheduled next run = %v, want %v", store.scheduledNextRunAt, want)
	}
	if !store.scheduledIncrementAttempts {
		t.Fatal("scheduled delete retry did not increment attempts")
	}
	if store.scheduledLastError != "node unavailable" {
		t.Fatalf("scheduled last error = %q, want node unavailable", store.scheduledLastError)
	}
}

func TestReconcilerReturnsRetryScheduleError(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:   "alloc-a",
			LifecycleState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING,
			NodeID:         "node-a",
			NodeTarget:     "node-a:24010",
		}},
		scheduleErr: errors.New("database unavailable"),
	}
	lifecycle := &fakeReconcileLifecycle{deleteErr: errors.New("node unavailable")}

	err := NewReconciler(store, lifecycle, "worker-a", func() time.Time { return now }).ReconcilePending(context.Background(), now)
	if err == nil {
		t.Fatal("ReconcilePending() error = nil, want schedule error")
	}
	if !errors.Is(err, store.scheduleErr) {
		t.Fatalf("ReconcilePending() error = %v, want schedule error", err)
	}
}

func TestReconcilerStartsQueuedAllocation(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:   "alloc-a",
			LifecycleState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND,
			NodeID:         "node-a",
			NodeTarget:     "node-a:24010",
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a"},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010"},
		},
	}
	lifecycle := &fakeReconcileLifecycle{}

	if err := NewReconciler(store, lifecycle, "worker-a", func() time.Time { return now }).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if lifecycle.created != 1 {
		t.Fatalf("create calls = %d, want 1", lifecycle.created)
	}
	if !lifecycle.createHasDeadline {
		t.Fatal("create context has no deadline")
	}
	remaining := time.Until(lifecycle.createDeadline)
	if remaining < 9*time.Minute || remaining > allocationkernel.CreateExecutionTimeout {
		t.Fatalf("create deadline remaining = %s, want approximately %s", remaining, allocationkernel.CreateExecutionTimeout)
	}
	if store.completedStartAllocationID != "alloc-a" {
		t.Fatalf("completed start allocation = %q, want alloc-a", store.completedStartAllocationID)
	}
	if store.completedStartConditions == nil {
		t.Fatal("successful start did not commit capability conditions with completion")
	}
	if store.failedAllocationID != "" {
		t.Fatalf("failed allocation after successful start: %q", store.failedAllocationID)
	}
}

func TestReconcilerReschedulesStartFailure(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:      "alloc-a",
			LifecycleState:    commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			ReconcileAttempts: 1,
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a"},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010"},
		},
	}
	lifecycle := &fakeReconcileLifecycle{createErr: errors.New("node unavailable")}

	if err := NewReconciler(store, lifecycle, "worker-a", func() time.Time { return now }).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if store.failedAllocationID != "" {
		t.Fatalf("failed allocation = %q, want empty before retry exhaustion", store.failedAllocationID)
	}
	if store.completedStartAllocationID != "" {
		t.Fatalf("completed start allocation = %q, want empty before retry exhaustion", store.completedStartAllocationID)
	}
	if store.scheduledAllocationID != "alloc-a" {
		t.Fatalf("scheduled allocation = %q, want alloc-a", store.scheduledAllocationID)
	}
	if store.scheduledIntent != allocationkernel.ReconcileIntentEnsurePresent {
		t.Fatalf("scheduled intent = %q, want %q", store.scheduledIntent, allocationkernel.ReconcileIntentEnsurePresent)
	}
	if want := now.Add(allocationkernel.CreateRetryDelay(2)); !store.scheduledNextRunAt.Equal(want) {
		t.Fatalf("scheduled next run = %v, want %v", store.scheduledNextRunAt, want)
	}
	if !store.scheduledIncrementAttempts {
		t.Fatal("scheduled start retry did not increment attempts")
	}
	if store.scheduledLastError != "node unavailable" {
		t.Fatalf("scheduled last error = %q, want node unavailable", store.scheduledLastError)
	}
}

func TestReconcilerMarksStartFailureAfterRetryExhaustion(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:      "alloc-a",
			LifecycleState:    commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			ReconcileAttempts: allocationkernel.CreateRetryMaxAttempts - 1,
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a"},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010"},
		},
	}
	lifecycle := &fakeReconcileLifecycle{createErr: errors.New("node unavailable")}

	if err := NewReconciler(store, lifecycle, "worker-a", func() time.Time { return now }).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if store.failedAllocationID != "alloc-a" {
		t.Fatalf("failed allocation = %q, want alloc-a", store.failedAllocationID)
	}
	if store.completedStartAllocationID != "" {
		t.Fatalf("completed create intent after it became delete intent: %q", store.completedStartAllocationID)
	}
	if store.scheduledAllocationID != "" {
		t.Fatalf("scheduled retry after exhaustion: %q", store.scheduledAllocationID)
	}
}

func TestReconcilerCancelsNodeOperationWhenClaimRenewalLosesOwnership(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:   "alloc-a",
			LifecycleState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND,
			NodeID:         "node-a",
			NodeTarget:     "node-a:24010",
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a"},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010"},
		},
		renewHeld:         false,
		rescheduleMissing: true,
	}
	lifecycle := &fakeReconcileLifecycle{waitForCancellation: true}
	reconciler := reconciler{
		store:        store,
		lifecycle:    lifecycle,
		owner:        "worker-a",
		claimTTL:     10 * time.Millisecond,
		claimRenewal: time.Millisecond,
		workerCount:  1,
		now:          func() time.Time { return now },
	}
	err := reconciler.ReconcilePending(context.Background(), now)
	if !errors.Is(err, allocationkernel.ErrReconcileClaimLost) {
		t.Fatalf("ReconcilePending() error = %v, want claim lost", err)
	}
	if store.renewCalls == 0 {
		t.Fatal("reconcile claim was not renewed")
	}
	if !lifecycle.createObservedCancellation {
		t.Fatal("node create continued after reconcile claim was lost")
	}
}

type fakeReconcileStore struct {
	items                      []allocationkernel.ReconcileItem
	start                      *runkernel.StartAllocation
	completedStartAllocationID string
	completedStartConditions   *capabilityv1.CapabilityConditionSet
	completedAllocationID      string
	failedAllocationID         string
	failedMessage              string
	markErr                    error
	scheduledAllocationID      string
	scheduledIntent            allocationkernel.ReconcileIntent
	scheduledNextRunAt         time.Time
	scheduledLastError         string
	scheduledIncrementAttempts bool
	scheduleErr                error
	rescheduleMissing          bool
	renewHeld                  bool
	renewCalls                 int
}

func (f *fakeReconcileStore) LoadStartAllocation(context.Context, string) (*runkernel.StartAllocation, error) {
	return f.start, nil
}

func (f *fakeReconcileStore) CompleteAllocationStart(_ context.Context, allocationID, _ string, conditions *capabilityv1.CapabilityConditionSet, _ time.Time) error {
	f.completedStartAllocationID = allocationID
	f.completedStartConditions = conditions
	return nil
}

func (f *fakeReconcileStore) CompleteAllocationRelease(_ context.Context, allocationID, _ string, _ time.Time) error {
	f.completedAllocationID = allocationID
	return nil
}

func (f *fakeReconcileStore) MarkAllocationCreateFailed(_ context.Context, allocationID, _ string, message string, _ time.Time) (*runv1.Run, error) {
	f.failedAllocationID = allocationID
	f.failedMessage = message
	if f.markErr != nil {
		return nil, f.markErr
	}
	return &runv1.Run{ID: "run-a", AllocationID: allocationID}, nil
}

func (f *fakeReconcileStore) ClaimDueReconcileItems(_ context.Context, owner string, _ int, _ time.Time, _ time.Duration) ([]allocationkernel.ReconcileItem, error) {
	items := f.items
	f.items = nil
	for i := range items {
		items[i].ClaimOwner = owner
	}
	return items, nil
}

func (f *fakeReconcileStore) RenewReconcileClaim(context.Context, string, string, time.Time, time.Duration) (bool, error) {
	f.renewCalls++
	return f.renewHeld, nil
}

func (f *fakeReconcileStore) ScheduleClaimedReconcile(_ context.Context, req allocationkernel.ScheduleReconcileRequest, _ string, _ time.Time) (bool, error) {
	err := f.recordSchedule(req)
	return !f.rescheduleMissing, err
}

func (f *fakeReconcileStore) WaitReconcileWork(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeReconcileStore) recordSchedule(req allocationkernel.ScheduleReconcileRequest) error {
	f.scheduledAllocationID = req.AllocationID
	f.scheduledIntent = req.Intent
	f.scheduledNextRunAt = req.NextRunAt
	f.scheduledLastError = req.LastReconcileError
	f.scheduledIncrementAttempts = req.IncrementAttempts
	return f.scheduleErr
}

type fakeReconcileLifecycle struct {
	deleted                    int
	created                    int
	deleteErr                  error
	createErr                  error
	createDeadline             time.Time
	createHasDeadline          bool
	waitForCancellation        bool
	createObservedCancellation bool
}

func (f *fakeReconcileLifecycle) CreateAllocation(ctx context.Context, _ string, _ *runv1.Run, _ *environmentv1.Environment, _ string, _ []*capabilityv1.CapabilityRequirement) (*capabilityv1.CapabilityConditionSet, error) {
	f.created++
	f.createDeadline, f.createHasDeadline = ctx.Deadline()
	if f.waitForCancellation {
		<-ctx.Done()
		f.createObservedCancellation = true
		return nil, ctx.Err()
	}
	return &capabilityv1.CapabilityConditionSet{}, f.createErr
}

func (f *fakeReconcileLifecycle) DeleteAllocation(context.Context, string, string, string, *allocationkernel.OutputSealing) error {
	f.deleted++
	return f.deleteErr
}
