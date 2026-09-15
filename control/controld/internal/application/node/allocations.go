package appnode

import (
	"context"
	"time"

	accessgrantkernel "github.com/cofy-x/axern/control/controld/internal/kernel/accessgrant"
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

type AllocationControl interface {
	BatchReportAllocationLifecycle(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationLifecycleObservation, now time.Time) ([]string, error)
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error
	ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error
	WatchAllocationAccessGrants(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*accessgrantkernel.Record, int64, error)
	ListNodeExecutionAllocationIDs(ctx context.Context, nodeID string) ([]string, error)
}

type RunAllocationStore interface {
	runkernel.AllocationReporter
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	WatchAllocationAccessGrants(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*accessgrantkernel.Record, int64, error)
}

func (n authoritativeAllocationAccess) BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error {
	return n.runStore.BatchReportAllocationCapabilityConditions(ctx, nodeID, reports, now)
}

func NewAuthoritative(runStore RunAllocationStore) AllocationControl {
	return authoritativeAllocationAccess{runStore: runStore}
}

type authoritativeAllocationAccess struct {
	runStore RunAllocationStore
}

func (n authoritativeAllocationAccess) BatchReportAllocationLifecycle(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationLifecycleObservation, now time.Time) ([]string, error) {
	if err := n.runStore.BatchReportAllocationLifecycle(ctx, nodeID, observations, now); err != nil {
		return nil, err
	}
	return nil, nil
}

func (n authoritativeAllocationAccess) WatchAllocationAccessGrants(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*accessgrantkernel.Record, int64, error) {
	return n.runStore.WatchAllocationAccessGrants(ctx, nodeID, afterRevision, now)
}

func (n authoritativeAllocationAccess) ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error {
	if err := n.runStore.ReconcileNodeInventory(ctx, snapshot, now); err != nil {
		return err
	}
	return nil
}

func (n authoritativeAllocationAccess) ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error {
	if err := n.runStore.ReconcileNodeUnavailable(ctx, nodeID, now); err != nil {
		return err
	}
	return nil
}

func (n authoritativeAllocationAccess) ListNodeExecutionAllocationIDs(ctx context.Context, nodeID string) ([]string, error) {
	return n.runStore.ListNodeExecutionAllocationIDs(ctx, nodeID)
}
