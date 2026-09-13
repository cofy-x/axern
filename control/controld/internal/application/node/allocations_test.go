package appnode

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestBatchReportAllocationLifecycleUsesRunStore(t *testing.T) {
	runs := &fakeRunAllocationStore{}
	control := NewAuthoritative(runs)
	observations := []*nodev1.AllocationLifecycleObservation{
		{AllocationID: " run-1 ", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE},
		{AllocationID: "unknown-owner", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE},
		{AllocationID: "missing", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE},
	}

	reconcileIDs, err := control.BatchReportAllocationLifecycle(context.Background(), "node-a", observations, time.Now().UTC())
	if err != nil {
		t.Fatalf("BatchReportAllocationLifecycle() error = %v", err)
	}
	if len(reconcileIDs) != 0 {
		t.Fatalf("reconcile IDs = %#v, want empty", reconcileIDs)
	}
	if got := allocationIDs(runs.observations); len(got) != 3 {
		t.Fatalf("run observations = %#v, want all allocation observations", got)
	}
}

type fakeRunAllocationStore struct {
	observations []*nodev1.AllocationLifecycleObservation
}

func (f *fakeRunAllocationStore) BatchReportAllocationLifecycle(_ context.Context, _ string, observations []*nodev1.AllocationLifecycleObservation, _ time.Time) error {
	f.observations = append(f.observations, observations...)
	return nil
}

func (f *fakeRunAllocationStore) BatchReportAllocationCapabilityConditions(context.Context, string, []*nodev1.AllocationCapabilityConditionReport, time.Time) error {
	return nil
}

func (f *fakeRunAllocationStore) ReconcileNodeInventory(context.Context, allocationkernel.NodeInventorySnapshot, time.Time) error {
	return nil
}

func (f *fakeRunAllocationStore) ReconcileNodeUnavailable(context.Context, string, time.Time) error {
	return nil
}

func (f *fakeRunAllocationStore) WatchExecutionLeases(context.Context, string, int64, time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	return nil, 0, nil
}

func allocationIDs(observations []*nodev1.AllocationLifecycleObservation) []string {
	ids := make([]string, 0, len(observations))
	for _, observation := range observations {
		ids = append(ids, observation.GetAllocationID())
	}
	return ids
}
