package appnode

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type AllocationControl interface {
	BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationStatusObservation, now time.Time) ([]string, error)
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error
	ReconcileNodeInventory(ctx context.Context, snapshot allocationkernel.NodeInventorySnapshot, now time.Time) error
	ReconcileNodeUnavailable(ctx context.Context, nodeID string, now time.Time) error
	WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error)
}

type AllocationOwnerResolver interface {
	ResolveAllocationOwners(ctx context.Context, allocationIDs []string) (map[string]string, error)
}

type RunAllocationStore interface {
	runkernel.AllocationReporter
	BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error
	BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error
	WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error)
}

func (n authoritativeAllocationAccess) BatchReportAllocationCapabilityConditions(ctx context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error {
	return n.runStore.BatchReportAllocationCapabilityConditions(ctx, nodeID, reports, now)
}

func (n authoritativeAllocationAccess) BatchReportAllocationMemoryObservations(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error {
	return n.runStore.BatchReportAllocationMemoryObservations(ctx, nodeID, observations, now)
}

func NewAuthoritative(owners AllocationOwnerResolver, runStore RunAllocationStore) AllocationControl {
	return authoritativeAllocationAccess{owners: owners, runStore: runStore}
}

type authoritativeAllocationAccess struct {
	owners   AllocationOwnerResolver
	runStore RunAllocationStore
}

func (n authoritativeAllocationAccess) BatchReportAllocationStatus(ctx context.Context, nodeID string, observations []*controlnodev1.AllocationStatusObservation, now time.Time) ([]string, error) {
	allocationIDs := make([]string, 0, len(observations))
	for _, observation := range observations {
		if observation != nil {
			allocationIDs = append(allocationIDs, observation.GetAllocationID())
		}
	}
	owners, err := n.owners.ResolveAllocationOwners(ctx, allocationIDs)
	if err != nil {
		return nil, err
	}
	runObservations := observationsForOwner(observations, owners, allocationkernel.OwnerRun)
	if len(runObservations) > 0 {
		if err := n.runStore.BatchReportAllocationStatus(ctx, nodeID, runObservations, now); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func observationsForOwner(observations []*controlnodev1.AllocationStatusObservation, owners map[string]string, ownerType string) []*controlnodev1.AllocationStatusObservation {
	out := make([]*controlnodev1.AllocationStatusObservation, 0, len(observations))
	for _, observation := range observations {
		if observation != nil && owners[strings.TrimSpace(observation.GetAllocationID())] == ownerType {
			out = append(out, observation)
		}
	}
	return out
}

func (n authoritativeAllocationAccess) WatchExecutionLeases(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	return n.runStore.WatchExecutionLeases(ctx, nodeID, afterRevision, now)
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
