package apprun

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestReconcilerCompletesDeleteRetry(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{items: []allocationkernel.ReconcileItem{{
		AllocationID: "alloc-a",
		Reason:       allocationkernel.ReconcileReasonDelete,
		NodeID:       "node-a",
		NodeTarget:   "node-a:24010",
		Attempt:      2,
	}}}
	lifecycle := &fakeReconcileLifecycle{}

	if err := NewReconciler(store, lifecycle).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if lifecycle.deleted != 1 {
		t.Fatalf("delete calls = %d, want 1", lifecycle.deleted)
	}
	if store.completedAllocationID != "alloc-a" || store.completedAttempt != 2 {
		t.Fatalf("completed = %q/%d, want alloc-a/2", store.completedAllocationID, store.completedAttempt)
	}
	if store.scheduledAllocationID != "" {
		t.Fatalf("scheduled retry for successful delete: %q", store.scheduledAllocationID)
	}
}

func TestReconcilerReschedulesDeleteRetryFailure(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{items: []allocationkernel.ReconcileItem{{
		AllocationID: "alloc-a",
		Reason:       allocationkernel.ReconcileReasonDelete,
		NodeID:       "node-a",
		NodeTarget:   "node-a:24010",
		Attempt:      2,
	}}}
	lifecycle := &fakeReconcileLifecycle{deleteErr: errors.New("node unavailable")}

	if err := NewReconciler(store, lifecycle).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if store.completedAllocationID != "" {
		t.Fatalf("completed allocation after failed delete: %q", store.completedAllocationID)
	}
	if store.scheduledAllocationID != "alloc-a" {
		t.Fatalf("scheduled allocation = %q, want alloc-a", store.scheduledAllocationID)
	}
	if store.scheduledReason != allocationkernel.ReconcileReasonDelete {
		t.Fatalf("scheduled reason = %q, want %q", store.scheduledReason, allocationkernel.ReconcileReasonDelete)
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
			AllocationID: "alloc-a",
			Reason:       allocationkernel.ReconcileReasonDelete,
			NodeID:       "node-a",
			NodeTarget:   "node-a:24010",
			Attempt:      2,
		}},
		scheduleErr: errors.New("database unavailable"),
	}
	lifecycle := &fakeReconcileLifecycle{deleteErr: errors.New("node unavailable")}

	err := NewReconciler(store, lifecycle).ReconcilePending(context.Background(), now)
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
			AllocationID: "alloc-a",
			Reason:       allocationkernel.ReconcileReasonCreate,
			NodeID:       "node-a",
			NodeTarget:   "node-a:24010",
			Attempt:      1,
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a", Attempt: 1},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010", Attempt: 1},
		},
	}
	lifecycle := &fakeReconcileLifecycle{}

	if err := NewReconciler(store, lifecycle).ReconcilePending(context.Background(), now); err != nil {
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
	if store.failedAllocationID != "" {
		t.Fatalf("failed allocation after successful start: %q", store.failedAllocationID)
	}
}

func TestReconcilerReschedulesStartFailure(t *testing.T) {
	now := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	store := &fakeReconcileStore{
		items: []allocationkernel.ReconcileItem{{
			AllocationID:      "alloc-a",
			Reason:            allocationkernel.ReconcileReasonCreate,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			Attempt:           1,
			ReconcileAttempts: 1,
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a", Attempt: 1},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010", Attempt: 1},
		},
	}
	lifecycle := &fakeReconcileLifecycle{createErr: errors.New("node unavailable")}

	if err := NewReconciler(store, lifecycle).ReconcilePending(context.Background(), now); err != nil {
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
	if store.scheduledReason != allocationkernel.ReconcileReasonCreate {
		t.Fatalf("scheduled reason = %q, want %q", store.scheduledReason, allocationkernel.ReconcileReasonCreate)
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
			Reason:            allocationkernel.ReconcileReasonCreate,
			NodeID:            "node-a",
			NodeTarget:        "node-a:24010",
			Attempt:           1,
			ReconcileAttempts: allocationkernel.CreateRetryMaxAttempts - 1,
		}},
		start: &runkernel.StartAllocation{
			Run:         &runv1.Run{ID: "run-a", AllocationID: "alloc-a", Attempt: 1},
			Environment: &environmentv1.Environment{ID: "env-a"},
			Allocation:  &runkernel.AllocationRecord{AllocationID: "alloc-a", NodeID: "node-a", NodeTarget: "node-a:24010", Attempt: 1},
		},
	}
	lifecycle := &fakeReconcileLifecycle{createErr: errors.New("node unavailable")}

	if err := NewReconciler(store, lifecycle).ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	if store.failedAllocationID != "alloc-a" {
		t.Fatalf("failed allocation = %q, want alloc-a", store.failedAllocationID)
	}
	if store.completedStartAllocationID != "alloc-a" {
		t.Fatalf("completed start allocation = %q, want alloc-a", store.completedStartAllocationID)
	}
	if store.scheduledAllocationID != "" {
		t.Fatalf("scheduled retry after exhaustion: %q", store.scheduledAllocationID)
	}
}

type fakeReconcileStore struct {
	items                      []allocationkernel.ReconcileItem
	start                      *runkernel.StartAllocation
	completedStartAllocationID string
	completedAllocationID      string
	completedAttempt           int64
	failedAllocationID         string
	failedMessage              string
	markErr                    error
	scheduledAllocationID      string
	scheduledReason            string
	scheduledNextRunAt         time.Time
	scheduledLastError         string
	scheduledIncrementAttempts bool
	scheduleErr                error
	rescheduleMissing          bool
}

func (f *fakeReconcileStore) LoadStartAllocation(context.Context, string) (*runkernel.StartAllocation, error) {
	return f.start, nil
}

func (f *fakeReconcileStore) CompleteAllocationStart(_ context.Context, allocationID string, _ time.Time) error {
	f.completedStartAllocationID = allocationID
	return nil
}

func (f *fakeReconcileStore) CompleteAllocationRelease(_ context.Context, allocationID string, attempt int64, _ time.Time) error {
	f.completedAllocationID = allocationID
	f.completedAttempt = attempt
	return nil
}

func (f *fakeReconcileStore) MarkAllocationCreateFailed(_ context.Context, allocationID string, message string, _ time.Time) (*runv1.Run, error) {
	f.failedAllocationID = allocationID
	f.failedMessage = message
	if f.markErr != nil {
		return nil, f.markErr
	}
	return &runv1.Run{ID: "run-a", AllocationID: allocationID}, nil
}

func (f *fakeReconcileStore) DueReconcileItems(context.Context, int, time.Time) ([]allocationkernel.ReconcileItem, error) {
	return f.items, nil
}

func (f *fakeReconcileStore) ScheduleReconcile(_ context.Context, req allocationkernel.ScheduleReconcileRequest, _ time.Time) error {
	return f.recordSchedule(req)
}

func (f *fakeReconcileStore) RescheduleReconcile(_ context.Context, req allocationkernel.ScheduleReconcileRequest, _ time.Time) (bool, error) {
	err := f.recordSchedule(req)
	return !f.rescheduleMissing, err
}

func (f *fakeReconcileStore) recordSchedule(req allocationkernel.ScheduleReconcileRequest) error {
	f.scheduledAllocationID = req.AllocationID
	f.scheduledReason = req.Reason
	f.scheduledNextRunAt = req.NextRunAt
	f.scheduledLastError = req.LastReconcileError
	f.scheduledIncrementAttempts = req.IncrementAttempts
	return f.scheduleErr
}

type fakeReconcileLifecycle struct {
	deleted           int
	created           int
	deleteErr         error
	createErr         error
	createDeadline    time.Time
	createHasDeadline bool
}

func (f *fakeReconcileLifecycle) CreateAllocation(ctx context.Context, _ string, _ *runv1.Run, _ *environmentv1.Environment, _ string) error {
	f.created++
	f.createDeadline, f.createHasDeadline = ctx.Deadline()
	return f.createErr
}

func (f *fakeReconcileLifecycle) DeleteAllocation(context.Context, string, string, int64, string) error {
	f.deleted++
	return f.deleteErr
}
