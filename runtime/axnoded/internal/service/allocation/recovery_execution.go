package allocation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
)

// markRecoveredTerminationLocked is the startup expiry
// fence before image/resource acquisition. It uses the same AllocationState
// and first-cause termination intent as the live watchdog, without creating a
// second admission record or publishing partially recovered ownership.
func (h *Controller) markRecoveredTerminationLocked(id string, now time.Time) (bool, error) {
	unlock := h.recordMutationLocks.Lock(id)
	defer unlock()
	var record *apipb.AllocationState
	h.stateMu.RLock()
	if state := h.allocationStates[id]; state != nil {
		record = cloneAllocationRecord(state.record)
	}
	h.stateMu.RUnlock()
	if record == nil {
		if h.store == nil {
			return false, fmt.Errorf("allocation %q has no durable recovery store", id)
		}
		record = &apipb.AllocationState{}
		if err := h.store.GetRecord(config.AllocationStateBucket, id, record); err != nil {
			return false, fmt.Errorf("load allocation %q execution authority: %w", id, err)
		}
	}
	if record.GetAllocationID() != id || strings.TrimSpace(record.GetNodeID()) == "" {
		return false, fmt.Errorf("allocation %q has no exact admitted recovery identity", id)
	}
	if _, err := classifyRecoveryRecord(record, now); err != nil {
		return false, fmt.Errorf("validate allocation %q execution authority: %w", id, err)
	}
	if record.GetTerminationDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return true, nil
	}
	if record.GetExecutionLeaseExpiresAtUnixNano() > now.UnixNano() {
		return false, nil
	}
	return true, h.markTerminationIntentLocked(record, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, "allocation execution lease expired")
}

// validateRecoveryActivationLocked additionally requires restored
// ownership. ValidateActivation also serves first starts, where missing state
// is checked by the admission workflow rather than this recovery boundary.
func (h *Controller) validateRecoveryActivationLocked(id string) (time.Time, error) {
	h.stateMu.RLock()
	state := h.allocationStates[id]
	complete := state != nil && state.record.GetAllocationID() == id && state.record.GetNodeID() != "" && state.record.GetEnforcementManifest() != nil && state.runtime != nil
	var deadline time.Time
	if state != nil {
		deadline = time.Unix(0, state.record.GetExecutionLeaseExpiresAtUnixNano())
	}
	h.stateMu.RUnlock()
	if !complete {
		return time.Time{}, fmt.Errorf("allocation %q has incomplete recovered execution ownership: %w", id, errord.ErrFailedPrecondition)
	}
	if !h.GuestProcessProfileVerified(id) {
		return time.Time{}, fmt.Errorf("allocation %q has no observed guest process profile: %w", id, errord.ErrFailedPrecondition)
	}
	return deadline, h.ValidateActivation(id)
}

// StopUnauthorizedRecovered checks the durable authority before resource recovery.
func (c *Controller) StopUnauthorizedRecovered(ctx context.Context, id string, state *contract.UnionContainerState) error {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	return c.checkRecoveredRuntimeLocked(ctx, id, state)
}

// checkRecoveredRuntimeLocked reconciles an observed runtime with its termination intent.
// Keep stopped runtimes in the inventory for the checkpoint and output-sealing paths.
func (c *Controller) checkRecoveredRuntimeLocked(ctx context.Context, id string, state *contract.UnionContainerState) error {
	terminated, err := c.markRecoveredTerminationLocked(id, time.Now().UTC())
	if err != nil {
		return err
	}
	options := contract.HandlerOptions{ContainerID: id, EnforcementManifest: c.EnforcementManifest(id)}
	switch {
	case terminated:
		if err := c.runscHandler.KillSandbox(ctx, options); err != nil {
			return fmt.Errorf("fail-stop recovered allocation %s: %w", id, err)
		}
	case state.Status == contract.ContainerStatusPaused:
		stopped, err := c.runscHandler.VerifySandboxStopped(ctx, options)
		if err != nil {
			return fmt.Errorf("verify recovered allocation %s: %w", id, err)
		}
		if !stopped {
			return nil
		}
	default:
		return nil
	}
	state.Status = contract.ContainerStatusExited
	state.InitProcessPid = 0
	return nil
}

// ResumeRecovered keeps the final authority check and runtime effect under one Allocation lock.
func (c *Controller) ResumeRecovered(ctx context.Context, id string, state *contract.UnionContainerState, recordExit func(context.Context, string, *contract.UnionContainerState) error) error {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	if err := c.checkRecoveredRuntimeLocked(ctx, id, state); err != nil {
		return err
	}
	if state.Status == contract.ContainerStatusExited {
		return recordExit(ctx, id, state)
	}
	deadline, err := c.validateRecoveryActivationLocked(id)
	if err != nil {
		return err
	}
	resumeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := c.runscHandler.ResumeContainer(resumeCtx, contract.HandlerOptions{ContainerID: id, EnforcementManifest: c.EnforcementManifest(id), GuestProcessProfileVerified: c.GuestProcessProfileVerified(id)}); err != nil {
		return fmt.Errorf("resume recovered allocation %s: %w", id, err)
	}
	return nil
}

func (c *Controller) PauseForShutdown(ctx context.Context, id string) error {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	return c.runscHandler.PauseContainer(ctx, contract.HandlerOptions{ContainerID: id})
}

// StopExpiredExecution rechecks an elapsed lease under the same lock as launch and recovery.
// The control plane owns final resource release after the workload is stopped.
func (c *Controller) StopExpiredExecution(ctx context.Context, id string, now time.Time) error {
	unlock, err := c.allocationLifecycleLocks.LockContext(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	expired, err := c.markExpiredExecutionLeaseLocked(id, now)
	if err != nil || !expired {
		return err
	}
	logrus.WithFields(logrus.Fields{"allocation_id": id, "termination_owner": "execution_lease"}).Warn("allocation execution lease expired; stopping sandbox fail-closed")
	return c.failStopWorkloadLocked(ctx, id)
}
