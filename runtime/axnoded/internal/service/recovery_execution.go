package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

// stopUnauthorizedRecoveredSandboxes runs after complete authority inspection
// and before live resource audits. A paused guest cannot serve sandboxd RPCs;
// host fail-stop preserves its artifacts for the normal Wait/checkpoint path.
func (h *sandboxService) stopUnauthorizedRecoveredSandboxes(ctx context.Context, inventory runtimeInventory) error {
	if !h.config.RuntimeConfig.NativeRecovery {
		for _, state := range inventory {
			if state != nil && state.Status == contract.ContainerStatusPaused {
				return fmt.Errorf("paused sandbox requires native_recovery")
			}
		}
		return nil
	}
	for _, id := range sortedRuntimeIDs(inventory) {
		state := inventory[id]
		if state == nil || (state.Status != contract.ContainerStatusPaused && state.Status != contract.ContainerStatusRunning) {
			continue
		}
		unlock, err := h.allocationController().LockAllocationLifecycleContext(ctx, id)
		if err != nil {
			return err
		}
		terminated, err := h.allocationController().MarkRecoveredExecutionTerminationWithLifecycleHeld(id, time.Now().UTC())
		if err == nil && !terminated && state.Status == contract.ContainerStatusPaused {
			var stopped bool
			stopped, err = h.runscHandler.VerifySandboxStopped(ctx, contract.HandlerOptions{ContainerID: id, EnforcementManifest: h.allocationController().EnforcementManifest(id)})
			if err == nil && stopped {
				state.Status = contract.ContainerStatusExited
				state.InitProcessPid = 0
			}
		}
		if err == nil && terminated {
			err = h.runscHandler.KillSandbox(ctx, contract.HandlerOptions{ContainerID: id, EnforcementManifest: h.allocationController().EnforcementManifest(id)})
			if err == nil {
				// KillSandbox promises confirmed kernel termination, not deletion.
				// Mutate the shared inventory entry so every partition sees it.
				state.Status = contract.ContainerStatusExited
				state.InitProcessPid = 0
			}
		}
		unlock()
		if err != nil {
			return fmt.Errorf("fail-stop recovered allocation %s: %w", id, err)
		}
	}
	return nil
}

// resumeRecoveredSandboxes is called only after persistent-state recovery and
// after the watchdog has started. It rechecks authority in the same lifecycle
// lane as expiry and Delete; a heartbeat cannot revive an elapsed deadline.
func (h *sandboxService) resumeRecoveredSandboxes(ctx context.Context) error {
	inventory, err := h.collectRuntimeInventory(ctx)
	if err != nil {
		return err
	}
	for _, id := range sortedRuntimeIDs(inventory) {
		state := inventory[id]
		if state.Status != contract.ContainerStatusPaused {
			continue
		}
		if !h.config.RuntimeConfig.NativeRecovery {
			return fmt.Errorf("paused sandbox requires native_recovery")
		}
		if h.executionLeaseCancel == nil {
			return fmt.Errorf("cannot resume allocation %s without execution lease watchdog", id)
		}
		if err := h.resumeRecoveredSandbox(ctx, id, state); err != nil {
			return err
		}
	}
	return nil
}

func (h *sandboxService) resumeRecoveredSandbox(ctx context.Context, id string, state *contract.UnionContainerState) error {
	controller := h.allocationController()
	unlock, err := controller.LockAllocationLifecycleContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	terminated, err := controller.MarkRecoveredExecutionTerminationWithLifecycleHeld(id, time.Now().UTC())
	if err != nil {
		return err
	}
	if terminated {
		if err := h.runscHandler.KillSandbox(ctx, contract.HandlerOptions{ContainerID: id, EnforcementManifest: controller.EnforcementManifest(id)}); err != nil {
			return fmt.Errorf("fail-stop allocation %s before resume: %w", id, err)
		}
		state.Status = contract.ContainerStatusExited
		state.InitProcessPid = 0
		if err := h.recoverTerminalRuntimeCheckpoints(ctx, runtimeInventory{id: state}); err != nil {
			return err
		}
		return h.seedTerminalAllocationLifecycleOutbox(map[string]struct{}{id: {}})
	}
	deadline, err := controller.ValidateRecoveryActivationWithLifecycleHeld(id)
	if err != nil {
		return err
	}
	resumeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := h.runscHandler.ResumeContainer(resumeCtx, contract.HandlerOptions{ContainerID: id}); err != nil {
		return fmt.Errorf("resume recovered allocation %s: %w", id, err)
	}
	return nil
}

// pauseLiveSandboxes closes the graceful shutdown boundary before stopping
// the lease watchdog. The native process supervisor covers abrupt death.
func (h *sandboxService) pauseLiveSandboxes(ctx context.Context) error {
	if h.runscHandler == nil || !h.config.RuntimeConfig.NativeRecovery {
		return nil
	}
	inventory, err := h.collectRuntimeInventory(ctx)
	if err != nil {
		return err
	}
	for _, id := range sortedRuntimeIDs(inventory) {
		state := inventory[id]
		switch state.Status {
		case contract.ContainerStatusPaused, contract.ContainerStatusCreated, contract.ContainerStatusExited:
			continue
		case contract.ContainerStatusRunning:
		default:
			return fmt.Errorf("cannot pause allocation %s in uncertain runtime state %q", id, state.Status)
		}
		unlock, err := h.allocationController().LockAllocationLifecycleContext(ctx, id)
		if err != nil {
			return err
		}
		err = h.runscHandler.PauseContainer(ctx, contract.HandlerOptions{ContainerID: id})
		unlock()
		if err != nil {
			return fmt.Errorf("pause allocation %s before node shutdown: %w", id, err)
		}
	}
	return nil
}

func sortedRuntimeIDs(inventory runtimeInventory) []string {
	ids := make([]string, 0, len(inventory))
	for id := range inventory {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
