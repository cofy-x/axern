package service

import (
	"context"
	"time"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
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
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		_, err := h.allocationController().Delete(ctx, &runtimev1.DeleteRequest{ID: allocationID, Timeout: 10})
		cancel()
		if err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Error("retry expired execution lease cleanup")
		}
	}
}
