package appnode

import (
	"context"
	"errors"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/sirupsen/logrus"
)

type AvailabilityReconciler interface {
	ReconcileUnavailableNodes(ctx context.Context, now time.Time) error
}

type AvailabilityReconcilerDeps struct {
	Nodes           nodekernel.Store
	Lifecycle       NodeLifecycleRegistry
	Allocations     AllocationControl
	HeartbeatWindow time.Duration
}

type NodeLifecycleRegistry interface {
	SyncLifecycle(nodeID string, lifecycle nodekernel.LifecycleStatus, retiredAt time.Time, reason string)
}

func NewAvailabilityReconciler(deps AvailabilityReconcilerDeps) AvailabilityReconciler {
	return availabilityReconciler{
		nodes:           deps.Nodes,
		lifecycle:       deps.Lifecycle,
		allocations:     deps.Allocations,
		heartbeatWindow: deps.HeartbeatWindow,
	}
}

type availabilityReconciler struct {
	nodes           nodekernel.Store
	lifecycle       NodeLifecycleRegistry
	allocations     AllocationControl
	heartbeatWindow time.Duration
}

func (r availabilityReconciler) ReconcileUnavailableNodes(ctx context.Context, now time.Time) error {
	if r.nodes == nil || r.allocations == nil || r.heartbeatWindow <= 0 {
		return nil
	}
	nodes, err := r.nodes.Load(ctx)
	if err != nil {
		return err
	}
	var reconcileErr error
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if r.lifecycle != nil {
			r.lifecycle.SyncLifecycle(node.NodeID, node.Lifecycle, node.RetiredAt, node.RetiredReason)
		}
		if !node.Active() || nodekernel.HeartbeatFresh(node.UpdatedAt, now, r.heartbeatWindow) {
			continue
		}
		err := r.allocations.ReconcileNodeUnavailable(ctx, node.NodeID, now)
		if err == nil {
			continue
		}
		logrus.WithError(err).WithField("node_id", node.NodeID).Warn("node unavailable reconciliation failed")
		reconcileErr = errors.Join(reconcileErr, err)
	}
	return reconcileErr
}
