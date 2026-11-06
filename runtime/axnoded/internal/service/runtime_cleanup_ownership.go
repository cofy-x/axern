package service

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

// runtimeCleanupOptions distinguishes admitted Allocations from node-owned
// conformance probes. A runtime projection can only be used for the latter
// after the exact checkpoint and durable typed resource lease agree; names and
// annotations never grant ownership. This permission cannot activate a guest.
func (h *sandboxService) runtimeCleanupOptions(ctx context.Context, id string) (contract.HandlerOptions, error) {
	options := contract.HandlerOptions{ContainerID: id, EnforcementManifest: h.allocationController().EnforcementManifest(id)}
	if !h.config.RuntimeConfig.NativeRecovery {
		return options, nil
	}
	records, err := h.allocationController().InspectRecoveryRecords()
	if err != nil {
		return options, fmt.Errorf("inspect cleanup admission for %s: %w", id, err)
	}
	if _, admitted := records.Intents[id]; admitted {
		return options, nil
	}
	if h.containerManager == nil {
		return options, fmt.Errorf("runtime %s has no durable cleanup ownership verifier", id)
	}
	checkpoint, err := h.containerManager.Get(id)
	if errors.Is(err, errord.ErrNotFound) {
		// No cleanup authority is granted. Native Delete permits this only if its
		// exact runtime lookup proves absence, which also supports probe preflight.
		options.EnforcementManifest = nil
		return options, nil
	}
	if err != nil || checkpoint == nil || checkpoint.ID != id || checkpoint.Metadata == nil || checkpoint.Status == nil {
		return options, fmt.Errorf("runtime %s has no exact cleanup checkpoint", id)
	}
	if h.conformanceRecovery == nil {
		return options, fmt.Errorf("runtime %s has no typed cleanup ownership verifier", id)
	}
	projection, err := h.runscHandler.AllocationEnforcementManifest(ctx, id)
	if err != nil {
		return options, fmt.Errorf("read runtime %s cleanup projection: %w", id, err)
	}
	if projection == nil || projection.GetBundlePath() != checkpoint.PATH || filepath.Base(checkpoint.PATH) != id {
		return options, fmt.Errorf("runtime %s cleanup projection does not match its checkpoint", id)
	}
	occupied, err := h.containerManager.CollectResourceByID(id)
	if err != nil {
		return options, fmt.Errorf("read runtime %s cleanup resource binding: %w", id, err)
	}
	if occupied.ID != id || occupied.Resources[resourcemanager.CgroupResourceName] == "" || occupied.Resources[resourcemanager.CgroupResourceName] != projection.GetCgroupPath() {
		return options, fmt.Errorf("runtime %s cleanup projection has no matching cgroup lease", id)
	}
	if err := h.conformanceRecovery.VerifyRuntimeConformanceBinding(id, projection); err != nil {
		return options, fmt.Errorf("verify runtime %s conformance cleanup ownership: %w", id, err)
	}
	options.EnforcementManifest = projection
	options.RuntimeConformanceCleanup = true
	return options, nil
}

// Serialize the runtime-only phase with start and graceful pause. Release this
// lane before Controller.Delete performs its own ordered, idempotent cleanup.
func (h *sandboxService) cleanupNativeConformanceRuntime(ctx context.Context, id string) error {
	unlock, err := h.allocationController().LockAllocationLifecycleContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	options, err := h.runtimeCleanupOptions(ctx, id)
	if err != nil {
		return err
	}
	options.ForceDelete = true
	if _, err := h.runscHandler.DeleteContainer(ctx, &apipb.DeleteContainerRequest{ID: id}, options); err != nil {
		return fmt.Errorf("delete native self-test runtime: %w", err)
	}
	return nil
}
