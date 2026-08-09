package runtime

import (
	"context"
	"errors"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
)

func (r *RuncServiceHandler) ReconcilePersistentStorage(ctx context.Context, runtimeInventory map[string]struct{}) error {
	return reconcilePersistentStorage(ctx, r.name, r.containerRoot, runtimeInventory, r.rootfsViews, r.writableCapacity)
}

func (r *RunscServiceHandler) ReconcilePersistentStorage(ctx context.Context, runtimeInventory map[string]struct{}) error {
	return reconcilePersistentStorage(ctx, r.name, r.containerRoot, runtimeInventory, r.rootfsViews, r.writableCapacity)
}

func reconcilePersistentStorage(
	ctx context.Context,
	runtimeName string,
	containerRoot string,
	runtimeInventory map[string]struct{},
	views rootfsview.Provider,
	capacity *writableCapacityManager,
) error {
	// Runtime-owned state may only be retained by the runtime's successful
	// inventory. Container metadata and allocation records can outlive a
	// crashed runtime or a rebuilt control plane. Copy the caller's generation
	// so providers cannot mutate the shared inventory.
	retained := make(map[string]struct{}, len(runtimeInventory))
	for id := range runtimeInventory {
		retained[id] = struct{}{}
	}

	var result error
	if reconciler, ok := views.(rootfsview.PersistentReconciler); ok {
		if err := reconciler.ReconcilePersistentViews(ctx, runtimeName, retained); err != nil {
			result = errors.Join(result, err)
		}
	}
	if err := capacity.ValidateRuntimeReservations(runtimeName, containerRoot, retained); err != nil {
		result = errors.Join(result, err)
	}
	if err := capacity.ReconcileRuntime(runtimeName, retained, func(id string) error {
		return views.Remove(ctx, id)
	}); err != nil {
		result = errors.Join(result, err)
	}
	return result
}
