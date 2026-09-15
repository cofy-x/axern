package runtime

import (
	"context"
	"errors"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
)

func (r *RunscServiceHandler) ReconcileRuntimeArtifacts(ctx context.Context, runtimeInventory map[string]struct{}) error {
	return reconcileRuntimeArtifacts(ctx, config.RuntimeNameRunsc, runtimeInventory, r.rootfsViews, r.writableCapacity)
}

func reconcileRuntimeArtifacts(
	ctx context.Context,
	runtimeName string,
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
	if reconciler, ok := views.(rootfsview.RuntimeArtifactReconciler); ok {
		if err := reconciler.ReconcileRuntimeViews(ctx, runtimeName, retained); err != nil {
			result = errors.Join(result, err)
		}
	}
	if err := capacity.Reconcile(retained, func(id string) error {
		return views.Remove(ctx, id)
	}); err != nil {
		result = errors.Join(result, err)
	}
	return result
}
