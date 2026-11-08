package allocation

import (
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

// MarkRecoveredExecutionTerminationWithLifecycleHeld is the startup expiry
// fence before image/resource acquisition. It uses the same AllocationState
// and first-cause termination intent as the live watchdog, without creating a
// second admission record or publishing partially recovered ownership.
func (h *Controller) MarkRecoveredExecutionTerminationWithLifecycleHeld(id string, now time.Time) (bool, error) {
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

// ValidateRecoveryActivationWithLifecycleHeld additionally requires restored
// ownership. ValidateActivation also serves first starts, where missing state
// is checked by the admission workflow rather than this recovery boundary.
func (h *Controller) ValidateRecoveryActivationWithLifecycleHeld(id string) (time.Time, error) {
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
