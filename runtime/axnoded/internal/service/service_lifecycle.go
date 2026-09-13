package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
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
	h.stopCapabilityRefresh()
	if h.capabilityReconcileCancel != nil {
		h.capabilityReconcileCancel()
	}
	h.capabilityReconcileWG.Wait()
	containers := h.containerManager.List()
	deleteErr := h.deleteAllocationsForShutdown(ctx, containers)

	if h.runscHandler != nil {
		h.runscHandler.ShutDown()
	}

	h.environmentCache.DrainRetained(ctx, environmentcache.RetentionReasonShutdown)
	h.environmentCache.Close()
	h.closeEgress()
	if err := h.containerManager.Stop(ctx); err != nil {
		deleteErr = errors.Join(deleteErr, fmt.Errorf("stop container manager: %w", err))
		// A monitor may still be checkpointing terminal state or invoking the
		// control-plane reporter. Keep the reporter and node-state store open;
		// the process supervisor will terminate after this bounded one-shot
		// shutdown failure, but this process must not close dependencies under
		// live users.
		logrus.WithError(deleteErr).Warn("sandbox service shutdown retained reporting and node state after container manager join failure")
		return deleteErr
	}
	h.inventoryCollector.Stop()
	h.controlPlaneReports.Stop()
	if err := h.store.Close(); err != nil {
		deleteErr = errors.Join(deleteErr, fmt.Errorf("close node state database: %w", err))
	}

	if h.config.RuntimeConfig.FilestoreDir != "" {
		if err := hostlinux.CleanupFilestore(h.config.RuntimeConfig.FilestoreDir, h.config.RuntimeConfig.FilestoreMode, h.config.RuntimeConfig.FilestoreLoopbackImage); err != nil {
			logrus.Warnf("shutdown: failed to clean runtime filestore: %v", err)
			deleteErr = errors.Join(deleteErr, fmt.Errorf("cleanup runtime filestore: %w", err))
		}
	}
	if deleteErr != nil {
		logrus.WithError(deleteErr).Warn("sandbox service shutdown completed with errors")
		return deleteErr
	}
	logrus.Info("sandbox service shutdown complete")
	return nil
}

func (h *sandboxService) deleteAllocationsForShutdown(ctx context.Context, containers []*container.Container) error {
	const workers = 8
	jobs := make(chan string)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var result error

	workerCount := min(workers, len(containers))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				if _, err := h.allocationController().Delete(ctx, &runtime.DeleteRequest{ID: id}); err != nil {
					wrapped := fmt.Errorf("delete container %s: %w", id, err)
					logrus.WithError(err).Warnf("shutdown: failed to delete container %s", id)
					errMu.Lock()
					result = errors.Join(result, wrapped)
					errMu.Unlock()
				}
			}
		}()
	}

	for _, item := range containers {
		if item == nil || item.Metadata == nil || item.ID == "" {
			continue
		}
		jobs <- item.ID
	}
	close(jobs)
	wg.Wait()
	return result
}
