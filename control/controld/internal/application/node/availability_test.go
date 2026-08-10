package appnode

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestAvailabilityReconcilerFailsOnlyStaleHeartbeatNodes(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	nodes := &fakeAvailabilityNodeStore{records: []*nodekernel.Record{
		{NodeID: "fresh", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now.Add(-5 * time.Second)},
		{NodeID: "stale", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now.Add(-30 * time.Second)},
	}}
	allocations := &fakeAvailabilityAllocations{}

	err := NewAvailabilityReconciler(AvailabilityReconcilerDeps{
		Nodes:           nodes,
		Allocations:     allocations,
		HeartbeatWindow: 15 * time.Second,
	}).ReconcileUnavailableNodes(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileUnavailableNodes() error = %v", err)
	}
	if got, want := allocations.unavailableNodeIDs, []string{"stale"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unavailable reconciliations = %#v, want %#v", got, want)
	}
}

func TestAvailabilityReconcilerContinuesAfterNodeFailure(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	nodes := &fakeAvailabilityNodeStore{records: []*nodekernel.Record{
		{NodeID: "stale-a", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now.Add(-30 * time.Second)},
		{NodeID: "stale-b", Lifecycle: nodekernel.LifecycleActive, UpdatedAt: now.Add(-45 * time.Second)},
	}}
	allocations := &fakeAvailabilityAllocations{errByNodeID: map[string]error{"stale-a": errors.New("database unavailable")}}

	err := NewAvailabilityReconciler(AvailabilityReconcilerDeps{
		Nodes:           nodes,
		Allocations:     allocations,
		HeartbeatWindow: 15 * time.Second,
	}).ReconcileUnavailableNodes(context.Background(), now)
	if err == nil {
		t.Fatal("ReconcileUnavailableNodes() error = nil, want aggregate error")
	}
	if got, want := allocations.unavailableNodeIDs, []string{"stale-a", "stale-b"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unavailable reconciliations = %#v, want %#v", got, want)
	}
}

func TestAvailabilityReconcilerSynchronizesLifecycleWithoutReconcilingRetiredNode(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	retiredAt := now.Add(-time.Minute)
	nodes := &fakeAvailabilityNodeStore{records: []*nodekernel.Record{{
		NodeID: "retired", Lifecycle: nodekernel.LifecycleRetired, UpdatedAt: now.Add(-time.Hour),
		RetiredAt: retiredAt, RetiredReason: "host removed",
	}}}
	lifecycle := &fakeLifecycleRegistry{}
	allocations := &fakeAvailabilityAllocations{}

	err := NewAvailabilityReconciler(AvailabilityReconcilerDeps{
		Nodes: nodes, Lifecycle: lifecycle, Allocations: allocations, HeartbeatWindow: 15 * time.Second,
	}).ReconcileUnavailableNodes(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileUnavailableNodes() error = %v", err)
	}
	if len(allocations.unavailableNodeIDs) != 0 {
		t.Fatalf("unavailable reconciliations = %#v, want none", allocations.unavailableNodeIDs)
	}
	if lifecycle.nodeID != "retired" || lifecycle.status != nodekernel.LifecycleRetired || !lifecycle.retiredAt.Equal(retiredAt) || lifecycle.reason != "host removed" {
		t.Fatalf("lifecycle sync = %#v", lifecycle)
	}
}

type fakeAvailabilityNodeStore struct {
	records []*nodekernel.Record
}

func (f *fakeAvailabilityNodeStore) Load(context.Context) ([]*nodekernel.Record, error) {
	return f.records, nil
}

func (f *fakeAvailabilityNodeStore) Register(context.Context, nodekernel.RegisterParams) (*nodekernel.Record, error) {
	panic("unexpected Register call")
}

func (f *fakeAvailabilityNodeStore) Report(context.Context, nodekernel.ReportParams) (*nodekernel.Record, error) {
	panic("unexpected Report call")
}

func (f *fakeAvailabilityNodeStore) Authenticate(context.Context, string, string) error {
	panic("unexpected Authenticate call")
}

type fakeAvailabilityAllocations struct {
	unavailableNodeIDs []string
	errByNodeID        map[string]error
}

func (f *fakeAvailabilityAllocations) BatchReportAllocationCapabilityConditions(context.Context, string, []*nodev1.AllocationCapabilityConditionReport, time.Time) error {
	panic("unexpected BatchReportAllocationCapabilityConditions call")
}

type fakeLifecycleRegistry struct {
	nodeID    string
	status    nodekernel.LifecycleStatus
	retiredAt time.Time
	reason    string
}

func (f *fakeLifecycleRegistry) SyncLifecycle(nodeID string, status nodekernel.LifecycleStatus, retiredAt time.Time, reason string) {
	f.nodeID = nodeID
	f.status = status
	f.retiredAt = retiredAt
	f.reason = reason
}

func (f *fakeAvailabilityAllocations) ReconcileNodeUnavailable(_ context.Context, nodeID string, _ time.Time) error {
	f.unavailableNodeIDs = append(f.unavailableNodeIDs, nodeID)
	if f.errByNodeID != nil {
		return f.errByNodeID[nodeID]
	}
	return nil
}

func (f *fakeAvailabilityAllocations) BatchReportAllocationStatus(context.Context, string, []*nodev1.AllocationStatusObservation, time.Time) ([]string, error) {
	panic("unexpected BatchReportAllocationStatus call")
}

func (f *fakeAvailabilityAllocations) ReconcileNodeInventory(context.Context, allocationkernel.NodeInventorySnapshot, time.Time) error {
	panic("unexpected ReconcileNodeInventory call")
}

func (f *fakeAvailabilityAllocations) WatchExecutionLeases(context.Context, string, int64, time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	panic("unexpected WatchExecutionLeases call")
}
