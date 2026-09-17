package service

import (
	"context"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
)

const executionLeaseWatchdogInterval = time.Second

func (h *sandboxService) startExecutionLeaseWatchdog(parent context.Context) {
	if h == nil || h.config.PluginConfig.ControlPlaneTargetValue() == "" {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	h.executionLeaseCancel = cancel
	h.executionLeaseWG.Add(1)
	go func() {
		defer h.executionLeaseWG.Done()
		ticker := time.NewTicker(executionLeaseWatchdogInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				h.stopExpiredExecutionLeases(ctx, now.UTC())
			}
		}
	}()
}

func (h *sandboxService) stopExpiredExecutionLeases(parent context.Context, now time.Time) {
	for _, allocationID := range h.allocationController().ExpiredExecutionLeaseAllocationIDs(now) {
		logrus.WithFields(logrus.Fields{
			"allocation_id":     allocationID,
			"termination_owner": "execution_lease",
		}).Warn("allocation execution lease expired; stopping sandbox fail-closed")
		if err := h.allocationController().MarkTerminationIntent(
			allocationID,
			commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED,
			"allocation execution lease expired",
		); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Error("persist expired execution lease termination intent")
			continue
		}
		if h.allocationRuntimeStopped(allocationID) {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		// Lease expiry owns fail-stop, not resource release. Keep the runtime
		// container metadata and durable AllocationState until controld observes
		// the terminal result and issues the authoritative cleanup request; that
		// request is also the output-sealing barrier.
		err := h.allocationController().FailStopWorkload(ctx, allocationID)
		cancel()
		if err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Error("retry expired execution lease fail-stop")
		}
	}
}
