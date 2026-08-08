package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
)

func (r *RuncServiceHandler) ReconcilePersistentStorage(ctx context.Context, activeIDs []string) error {
	return reconcilePersistentStorage(ctx, r.name, r.containerRoot, activeIDs, r.ListContainers, r.rootfsViews, r.writableCapacity)
}

func (r *RunscServiceHandler) ReconcilePersistentStorage(ctx context.Context, activeIDs []string) error {
	return reconcilePersistentStorage(ctx, r.name, r.containerRoot, activeIDs, r.ListContainers, r.rootfsViews, r.writableCapacity)
}

func reconcilePersistentStorage(
	ctx context.Context,
	runtimeName string,
	containerRoot string,
	activeIDs []string,
	list func(context.Context, contract.HandlerOptions) ([]*contract.UnionContainerState, error),
	views rootfsview.Provider,
	capacity *writableCapacityManager,
) error {
	runtimeContainers, err := list(ctx, contract.HandlerOptions{})
	if err != nil {
		return fmt.Errorf("list %s containers before storage reconciliation: %w", runtimeName, err)
	}
	retained := make(map[string]struct{}, len(activeIDs)+len(runtimeContainers))
	for _, id := range activeIDs {
		if id != "" {
			retained[id] = struct{}{}
		}
	}
	for _, state := range runtimeContainers {
		if state != nil && state.ID != "" {
			retained[state.ID] = struct{}{}
		}
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
