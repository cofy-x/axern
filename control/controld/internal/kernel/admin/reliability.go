package adminkernel

import (
	"fmt"
	"strings"
	"time"

	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
)

type ReliabilityStatus string

const (
	ReliabilityStatusOK       ReliabilityStatus = "ok"
	ReliabilityStatusDegraded ReliabilityStatus = "degraded"
)

type ReliabilitySignalCode string

const (
	ReliabilitySignalConsistencyIssues          ReliabilitySignalCode = "consistency_issues"
	ReliabilitySignalAllocationLifecycleRetries ReliabilitySignalCode = "allocation_lifecycle_retries"
	ReliabilitySignalReconcileFailures          ReliabilitySignalCode = "reconcile_failures"
	ReliabilitySignalStorageBindings            ReliabilitySignalCode = "storage_bindings"
	ReliabilitySignalNodeVolumeManagers         ReliabilitySignalCode = "node_volume_managers"
	ReliabilitySignalNodeFleet                  ReliabilitySignalCode = "node_fleet"
)

type AllocationLifecycleRetryCounts struct {
	Total int64
	Due   int64
}

type ReliabilitySignal struct {
	Code    ReliabilitySignalCode
	Message string
}

type ReliabilityHealth struct {
	Status                        ReliabilityStatus
	Consistency                   consistencykernel.Snapshot
	AllocationLifecycleRetries    int64
	DueAllocationLifecycleRetries int64
	ReconcileUnhealthyComponents  int64
	StorageBindingHealth          StorageBindingHealth
	NodeVolumeHealth              NodeVolumeHealth
	NodeFleetHealth               NodeFleetHealth
	ReconcileComponents           []reconcilekernel.ComponentHealth
	Signals                       []ReliabilitySignal
}

type StorageBindingHealth struct {
	Unavailable            bool
	Error                  string
	FailedBindings         int64
	ReleasingBindings      int64
	StuckReleasingBindings int64
	InconsistentClaims     int64
	InvalidBindings        int64
	DeletingClaims         int64
	StuckDeletingClaims    int64
}

type NodeVolumeHealth struct {
	UnhealthyNodes                int64
	PublishedVolumes              int64
	LastReconcileStaleAllocations int64
	LastReconcileInvalidVolumes   int64
	Error                         string
}

type NodeFleetHealth struct {
	Observed            bool
	Unavailable         bool
	Error               string
	ActiveNodes         int64
	ReadyNodes          int64
	StaleHeartbeatNodes int64
	StaleSummaryNodes   int64
	NotReadyNodes       int64
}

func BuildReliabilityHealth(consistency consistencykernel.Snapshot, retryCounts AllocationLifecycleRetryCounts, reconcile reconcilekernel.HealthSnapshot, reconcileStuckAfter time.Duration, storage StorageBindingHealth, nodeVolumes NodeVolumeHealth, nodeFleet NodeFleetHealth, now time.Time) ReliabilityHealth {
	signals := make([]ReliabilitySignal, 0, 6)
	if consistency.Status != consistencykernel.StatusOK {
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalConsistencyIssues,
			Message: fmt.Sprintf("control-plane consistency has %d issue(s)", consistency.Counts.Issues),
		})
	}
	if retryCounts.Total > 0 {
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalAllocationLifecycleRetries,
			Message: fmt.Sprintf("%d allocation lifecycle retry item(s), %d due", retryCounts.Total, retryCounts.Due),
		})
	}
	unhealthyReconcile, stuckReconcile := reconcilekernel.CountUnhealthyComponents(reconcile, now, reconcileStuckAfter)
	if unhealthyReconcile > 0 {
		message := fmt.Sprintf("%d reconcile component(s) are currently failing", unhealthyReconcile)
		if stuckReconcile > 0 {
			message = fmt.Sprintf("%d reconcile component(s) are currently failing or stuck (%d stuck)", unhealthyReconcile, stuckReconcile)
		}
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalReconcileFailures,
			Message: message,
		})
	}
	if storage.Unavailable {
		message := strings.TrimSpace(storage.Error)
		if message == "" {
			message = "storage binding health is unavailable"
		}
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalStorageBindings,
			Message: message,
		})
	} else if storage.FailedBindings > 0 || storage.StuckReleasingBindings > 0 || storage.StuckDeletingClaims > 0 || storage.InconsistentClaims > 0 || storage.InvalidBindings > 0 {
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalStorageBindings,
			Message: storageBindingSignalMessage(storage),
		})
	}
	if nodeVolumes.UnhealthyNodes > 0 {
		message := fmt.Sprintf("%d node(s) report unhealthy volume manager (%d published volume(s), last reconcile: %d stale allocation(s), %d invalid volume(s))",
			nodeVolumes.UnhealthyNodes,
			nodeVolumes.PublishedVolumes,
			nodeVolumes.LastReconcileStaleAllocations,
			nodeVolumes.LastReconcileInvalidVolumes,
		)
		if strings.TrimSpace(nodeVolumes.Error) != "" {
			message = fmt.Sprintf("%s: %s", message, strings.TrimSpace(nodeVolumes.Error))
		}
		message += "; inspect node volumed health before retrying affected bindings"
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalNodeVolumeManagers,
			Message: message,
		})
	}
	if nodeFleet.Observed && (nodeFleet.Unavailable || nodeFleet.ActiveNodes == 0 || nodeFleet.StaleHeartbeatNodes > 0 || nodeFleet.StaleSummaryNodes > 0 || nodeFleet.NotReadyNodes > 0) {
		message := fmt.Sprintf("node fleet has %d active node(s), %d ready, %d stale heartbeat, %d stale summary, %d not ready; inspect node list and retire permanently removed nodes",
			nodeFleet.ActiveNodes, nodeFleet.ReadyNodes, nodeFleet.StaleHeartbeatNodes, nodeFleet.StaleSummaryNodes, nodeFleet.NotReadyNodes)
		if nodeFleet.Unavailable {
			message = "node fleet health is unavailable"
			if strings.TrimSpace(nodeFleet.Error) != "" {
				message += ": " + strings.TrimSpace(nodeFleet.Error)
			}
		}
		signals = append(signals, ReliabilitySignal{
			Code:    ReliabilitySignalNodeFleet,
			Message: message,
		})
	}
	status := ReliabilityStatusOK
	if len(signals) > 0 {
		status = ReliabilityStatusDegraded
	}
	return ReliabilityHealth{
		Status:                        status,
		Consistency:                   consistency,
		AllocationLifecycleRetries:    retryCounts.Total,
		DueAllocationLifecycleRetries: retryCounts.Due,
		ReconcileUnhealthyComponents:  unhealthyReconcile,
		StorageBindingHealth:          storage,
		NodeVolumeHealth:              nodeVolumes,
		NodeFleetHealth:               nodeFleet,
		ReconcileComponents:           append([]reconcilekernel.ComponentHealth(nil), reconcile.Components...),
		Signals:                       signals,
	}
}

func storageBindingSignalMessage(storage StorageBindingHealth) string {
	message := fmt.Sprintf("%d failed storage binding(s), %d stuck releasing", storage.FailedBindings, storage.StuckReleasingBindings)
	if storage.FailedBindings > 0 {
		message += "; list failed bindings and retry after fixing the node/storage cause"
	}
	if storage.StuckReleasingBindings > 0 {
		message += "; inspect release observations for stuck bindings"
	}
	if storage.StuckDeletingClaims > 0 {
		message += fmt.Sprintf(", %d stuck deleting claim(s); inspect node reachability and volume reclaim errors", storage.StuckDeletingClaims)
	}
	if storage.InconsistentClaims > 0 || storage.InvalidBindings > 0 {
		message += fmt.Sprintf("; storage consistency has %d inconsistent claim(s), %d invalid binding(s)", storage.InconsistentClaims, storage.InvalidBindings)
	}
	return message
}
