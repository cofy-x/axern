package service

import (
	"context"
	"errors"
	"fmt"

	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/sirupsen/logrus"
)

func (h *sandboxService) Ready() bool {
	return h.ready.Load()
}

func (h *sandboxService) Run(ctx context.Context) error {
	logrus.Infof("sandbox service run at %s", h.config.RootDir)
	h.ready.Store(false)
	if h.capabilityReconcileCancel != nil {
		h.capabilityReconcileCancel()
	}
	h.capabilityReconcileCtx, h.capabilityReconcileCancel = context.WithCancel(context.Background())
	if err := h.controlPlaneReports.ReplayDurableAllocationLifecycles(); err != nil {
		return fmt.Errorf("replay durable allocation lifecycle outbox: %w", err)
	}
	h.inventoryCollector.Start()
	h.controlPlaneReports.Start()
	h.startExecutionLeaseWatchdog(ctx)
	h.startCapabilityRefresh(ctx)
	h.startPeriodicCapabilityAudit()
	h.environmentCache.Start()
	go h.containerManager.Start()
	return nil
}

func (h *sandboxService) Shutdown(ctx context.Context) error {
	h.shutdownOnce.Do(func() {
		h.shutdownErr = h.shutdown(ctx)
	})
	return h.shutdownErr
}

func (h *sandboxService) shutdown(ctx context.Context) error {
	logrus.Info("sandbox service shutting down")
	h.ready.Store(false)
	if h.executionLeaseCancel != nil {
		h.executionLeaseCancel()
	}
	h.executionLeaseWG.Wait()
	h.stopCapabilityRefresh()
	if h.capabilityReconcileCancel != nil {
		h.capabilityReconcileCancel()
	}
	h.capabilityReconcileWG.Wait()
	// Service shutdown is not an Allocation lifecycle transition. Preserve live
	// runsc sandboxes and their durable resource bindings so the next axnoded
	// process can recover them. Only an explicit control-plane DeleteAllocation
	// or an expired execution authorization may tear them down.
	var shutdownErr error

	if h.runscHandler != nil {
		h.runscHandler.ShutDown()
	}

	h.environmentCache.DrainRetained(ctx, environmentcache.RetentionReasonShutdown)
	h.environmentCache.Close()
	h.closeEgress()
	if err := h.containerManager.Stop(ctx); err != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("stop container manager: %w", err))
		// A monitor may still be checkpointing terminal state or invoking the
		// control-plane reporter. Keep the reporter and node-state store open;
		// the process supervisor will terminate after this bounded one-shot
		// shutdown failure, but this process must not close dependencies under
		// live users.
		logrus.WithError(shutdownErr).Warn("sandbox service shutdown retained reporting and node state after container manager join failure")
		return shutdownErr
	}
	h.inventoryCollector.Stop()
	h.controlPlaneReports.Stop()
	if err := h.store.Close(); err != nil {
		shutdownErr = errors.Join(shutdownErr, fmt.Errorf("close node state database: %w", err))
	}

	if shutdownErr != nil {
		logrus.WithError(shutdownErr).Warn("sandbox service shutdown completed with errors")
		return shutdownErr
	}
	logrus.Info("sandbox service shutdown complete")
	return nil
}
