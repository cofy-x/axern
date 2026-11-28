package service

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

const executionLeaseWatchdogInterval = time.Second

func (h *sandboxService) startExecutionLeaseWatchdog(parent context.Context) {
	if h == nil || h.config.PluginConfig.ControlPlaneTargetValue() == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
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
		h.stopExpiredExecutionLease(parent, allocationID, now)
	}
}

func (h *sandboxService) stopExpiredExecutionLease(parent context.Context, allocationID string, now time.Time) {
	controller := h.allocationController()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := controller.StopExpiredExecution(ctx, allocationID, now); err != nil && ctx.Err() == nil {
		logrus.WithError(err).WithField("allocation_id", allocationID).Error("retry expired execution lease fail-stop")
	}
}
