package appadmin

import (
	"context"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
)

type ReliabilityStore interface {
	ConsistencySnapshot(ctx context.Context, now time.Time) (consistencykernel.Snapshot, error)
	CountAllocationLifecycleRetries(ctx context.Context, now time.Time) (adminkernel.AllocationLifecycleRetryCounts, error)
}

type NodeHealthSource interface {
	NodeHealth(ctx context.Context, now time.Time) (adminkernel.NodeFleetHealth, error)
}

type ReliabilityControl struct {
	store               ReliabilityStore
	reconcileHealth     func() reconcilekernel.HealthSnapshot
	reconcileStuckAfter time.Duration
	nodeHealth          NodeHealthSource
}

func NewReliabilityControl(store ReliabilityStore, reconcileHealth func() reconcilekernel.HealthSnapshot, reconcileStuckAfter time.Duration, nodeHealth NodeHealthSource) ReliabilityControl {
	return ReliabilityControl{store: store, reconcileHealth: reconcileHealth, reconcileStuckAfter: reconcileStuckAfter, nodeHealth: nodeHealth}
}

func (c ReliabilityControl) ConsistencySnapshot(ctx context.Context, now time.Time) (consistencykernel.Snapshot, error) {
	return c.store.ConsistencySnapshot(ctx, now)
}

func (c ReliabilityControl) Health(ctx context.Context, now time.Time) (adminkernel.ReliabilityHealth, error) {
	consistency, err := c.store.ConsistencySnapshot(ctx, now)
	if err != nil {
		return adminkernel.ReliabilityHealth{}, err
	}
	retryCounts, err := c.store.CountAllocationLifecycleRetries(ctx, now)
	if err != nil {
		return adminkernel.ReliabilityHealth{}, err
	}
	reconcileHealth := reconcilekernel.EmptyHealthSnapshot()
	if c.reconcileHealth != nil {
		reconcileHealth = c.reconcileHealth()
	}
	nodeFleetHealth := adminkernel.NodeFleetHealth{}
	if c.nodeHealth != nil {
		nodeFleetHealth, err = c.nodeHealth.NodeHealth(ctx, now)
		if err != nil {
			nodeFleetHealth = adminkernel.NodeFleetHealth{Observed: true, Unavailable: true, Error: err.Error()}
		}
	}
	return adminkernel.BuildReliabilityHealth(consistency, retryCounts, reconcileHealth, c.reconcileStuckAfter, nodeFleetHealth, now), nil
}
