package appnode

import (
	"context"
	"errors"
	"testing"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

func TestAvailabilityReconcilerFailsOnlyStaleHeartbeatNodes(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	nodes := &fakeAvailabilityNodeStore{records: []*nodekernel.Record{
		{NodeID: "fresh", Lifecycle: nodekernel.LifecycleActive, LastHeartbeatAt: now.Add(-5 * time.Second)},
		{NodeID: "stale", Lifecycle: nodekernel.LifecycleActive, LastHeartbeatAt: now.Add(-30 * time.Second)},
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
		{NodeID: "stale-a", Lifecycle: nodekernel.LifecycleActive, LastHeartbeatAt: now.Add(-30 * time.Second)},
		{NodeID: "stale-b", Lifecycle: nodekernel.LifecycleActive, LastHeartbeatAt: now.Add(-45 * time.Second)},
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
		NodeID: "retired", Lifecycle: nodekernel.LifecycleRetired, LastHeartbeatAt: now.Add(-time.Hour),
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

func (f *fakeAvailabilityNodeStore) Report(context.Context, nodekernel.ReportParams) (*nodekernel.Record, error) {
	panic("unexpected Report call")
}

func (f *fakeAvailabilityNodeStore) RequireActive(context.Context, string) error {
	panic("unexpected Authenticate call")
}

type fakeAvailabilityAllocations struct {
	unavailableNodeIDs []string
	errByNodeID        map[string]error
	executionIDs       []string
	executionListCalls int
	inventoryCalls     int
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

func (f *fakeAvailabilityAllocations) BatchReportAllocationLifecycle(context.Context, string, []*nodev1.AllocationLifecycleObservation, time.Time) ([]string, error) {
	panic("unexpected BatchReportAllocationLifecycle call")
}

func (f *fakeAvailabilityAllocations) ReconcileNodeInventory(context.Context, allocationkernel.NodeInventorySnapshot, time.Time) error {
	f.inventoryCalls++
	return nil
}

func (f *fakeAvailabilityAllocations) WatchAllocationAccessGrants(context.Context, string, int64, time.Time) ([]*accessgrantkernel.Record, int64, error) {
	panic("unexpected WatchAllocationAccessGrants call")
}

func (f *fakeAvailabilityAllocations) ListNodeExecutionAllocationIDs(context.Context, string) ([]string, error) {
	f.executionListCalls++
	return append([]string(nil), f.executionIDs...), nil
}

func TestAvailabilityReconcilesRevokedNodeWithFreshHeartbeat(t *testing.T) {
	now := time.Now()
	allocations := &fakeAvailabilityAllocations{}
	err := NewAvailabilityReconciler(AvailabilityReconcilerDeps{
		Nodes:       &fakeAvailabilityNodeStore{records: []*nodekernel.Record{{NodeID: "revoked", Lifecycle: nodekernel.LifecycleRevoked, LastHeartbeatAt: now}}},
		Allocations: allocations, HeartbeatWindow: time.Minute,
	}).ReconcileUnavailableNodes(context.Background(), now)
	if err != nil || len(allocations.unavailableNodeIDs) != 1 {
		t.Fatalf("revoked cleanup not driven: %v %v", allocations.unavailableNodeIDs, err)
	}
}
