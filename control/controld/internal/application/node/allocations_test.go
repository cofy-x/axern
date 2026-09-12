package appnode

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestBatchReportAllocationStatusUsesRunStore(t *testing.T) {
	runs := &fakeRunAllocationStore{}
	control := NewAuthoritative(runs)
	observations := []*nodev1.AllocationStatusObservation{
		{AllocationID: " run-1 ", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
		{AllocationID: "unknown-owner", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
		{AllocationID: "missing", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
	}

	reconcileIDs, err := control.BatchReportAllocationStatus(context.Background(), "node-a", observations, time.Now().UTC())
	if err != nil {
		t.Fatalf("BatchReportAllocationStatus() error = %v", err)
	}
	if len(reconcileIDs) != 0 {
		t.Fatalf("reconcile IDs = %#v, want empty", reconcileIDs)
	}
	if got := allocationIDs(runs.observations); len(got) != 3 {
		t.Fatalf("run observations = %#v, want all allocation observations", got)
	}
}

type fakeRunAllocationStore struct {
	observations []*nodev1.AllocationStatusObservation
}

func (f *fakeRunAllocationStore) BatchReportAllocationStatus(_ context.Context, _ string, observations []*nodev1.AllocationStatusObservation, _ time.Time) error {
	f.observations = append(f.observations, observations...)
	return nil
}

func (f *fakeRunAllocationStore) BatchReportAllocationCapabilityConditions(context.Context, string, []*nodev1.AllocationCapabilityConditionReport, time.Time) error {
	return nil
}

func (f *fakeRunAllocationStore) BatchReportAllocationMemoryObservations(context.Context, string, []*nodev1.AllocationMemoryObservation, time.Time) error {
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

func allocationIDs(observations []*nodev1.AllocationStatusObservation) []string {
	ids := make([]string, 0, len(observations))
	for _, observation := range observations {
		ids = append(ids, observation.GetAllocationID())
	}
	return ids
}
