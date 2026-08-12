package appnode

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestBatchReportAllocationStatusRoutesOwnersOnce(t *testing.T) {
	owners := &fakeAllocationOwnerResolver{owners: map[string]string{
		"run-1":     allocationkernel.OwnerRun,
		"service-1": allocationkernel.OwnerService,
	}}
	runs := &fakeRunAllocationStore{}
	services := &fakeServiceAllocationReporter{reconcileServiceIDs: []string{"svc-1"}}
	control := NewAuthoritative(owners, runs, services)
	observations := []*nodev1.AllocationStatusObservation{
		{AllocationID: " run-1 ", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
		{AllocationID: "service-1", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
		{AllocationID: "missing", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
	}

	reconcileServiceIDs, err := control.BatchReportAllocationStatus(context.Background(), "node-a", observations, time.Now().UTC())
	if err != nil {
		t.Fatalf("BatchReportAllocationStatus() error = %v", err)
	}
	if len(reconcileServiceIDs) != 1 || reconcileServiceIDs[0] != "svc-1" {
		t.Fatalf("reconcile service IDs = %#v, want [svc-1]", reconcileServiceIDs)
	}
	if owners.calls != 1 {
		t.Fatalf("owner resolver calls = %d, want 1", owners.calls)
	}
	if got := allocationIDs(runs.observations); len(got) != 1 || got[0] != " run-1 " {
		t.Fatalf("run observations = %#v, want canonical owner routing to preserve payload", got)
	}
	if got := allocationIDs(services.observations); len(got) != 1 || got[0] != "service-1" {
		t.Fatalf("service observations = %#v, want [service-1]", got)
	}
}

type fakeAllocationOwnerResolver struct {
	owners map[string]string
	calls  int
}

func (f *fakeAllocationOwnerResolver) ResolveAllocationOwners(_ context.Context, _ []string) (map[string]string, error) {
	f.calls++
	return f.owners, nil
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

type fakeServiceAllocationReporter struct {
	observations        []*nodev1.AllocationStatusObservation
	reconcileServiceIDs []string
}

func (f *fakeServiceAllocationReporter) BatchReportAllocationStatus(_ context.Context, _ string, observations []*nodev1.AllocationStatusObservation, _ time.Time) (servicekernel.AllocationStatusBatchResult, error) {
	f.observations = append(f.observations, observations...)
	return servicekernel.AllocationStatusBatchResult{ReconcileServiceIDs: f.reconcileServiceIDs}, nil
}

func (f *fakeServiceAllocationReporter) ReconcileNodeInventory(context.Context, allocationkernel.NodeInventorySnapshot, time.Time) error {
	return nil
}

func (f *fakeServiceAllocationReporter) ReconcileNodeUnavailable(context.Context, string, time.Time) error {
	return nil
}

func allocationIDs(observations []*nodev1.AllocationStatusObservation) []string {
	ids := make([]string, 0, len(observations))
	for _, observation := range observations {
		ids = append(ids, observation.GetAllocationID())
	}
	return ids
}
