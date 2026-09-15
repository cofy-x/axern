package allocation

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

// HasAdmittedAllocation reports whether the sole Allocation recovery record
// carries an explicit node admission. Runtime state and metadata never imply it.
func (h *Controller) HasAdmittedAllocation(allocationID string) bool {
	if h == nil {
		return false
	}
	allocationID = strings.TrimSpace(allocationID)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[allocationID]
	return state != nil && state.record != nil && state.record.GetAllocationID() == allocationID && strings.TrimSpace(state.record.GetNodeID()) != ""
}

func (h *Controller) AdmittedAllocationMatches(allocationID, nodeID string) bool {
	if h == nil {
		return false
	}
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[allocationID]
	return state != nil && state.record != nil && state.record.GetAllocationID() == allocationID && state.record.GetNodeID() == nodeID && nodeID != ""
}

func (h *Controller) AdmittedAllocationIDs() []string {
	if h == nil {
		return nil
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	ids := make([]string, 0, len(h.allocationStates))
	for allocationID, state := range h.allocationStates {
		if state != nil && state.record != nil && state.record.GetAllocationID() == allocationID && strings.TrimSpace(state.record.GetNodeID()) != "" {
			ids = append(ids, allocationID)
		}
	}
	sort.Strings(ids)
	return ids
}

// ValidateOperatorExecution proves that an operator request addresses the
// exact live runtime projection owned by one AllocationState. Container
// presence alone is never sufficient authority for node-local exec.
func (h *Controller) ValidateOperatorExecution(allocationID string, now time.Time) error {
	allocationID = strings.TrimSpace(allocationID)
	if h == nil || allocationID == "" {
		return errord.ErrInvalidArgument
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[allocationID]
	if state == nil || state.record == nil || state.record.GetAllocationID() != allocationID {
		return fmt.Errorf("allocation %q has no authoritative AllocationState: %w", allocationID, errord.ErrNotFound)
	}
	if !validStartRequestDigest(state.record.GetAllocationRequestDigest()) || state.record.GetEnforcementManifest() == nil || state.runtime == nil {
		return fmt.Errorf("allocation %q runtime identity is incomplete: %w", allocationID, errord.ErrFailedPrecondition)
	}
	if state.record.GetTerminationDiagnosticCode() != 0 {
		return fmt.Errorf("allocation %q is terminating: %w", allocationID, errord.ErrFailedPrecondition)
	}
	if strings.TrimSpace(state.record.GetNodeID()) != "" {
		expiresAt := state.record.GetExecutionLeaseExpiresAtUnixNano()
		if expiresAt <= 0 || !time.Unix(0, expiresAt).After(now) {
			return fmt.Errorf("allocation %q has no valid execution lease: %w", allocationID, errord.ErrFailedPrecondition)
		}
	}
	return nil
}

// ValidateOperatorRecovery proves only durable identity and runtime ownership.
// It intentionally does not require a live lease or a RUNNING status because
// break-glass cleanup exists for partitioned and partially terminated work.
func (h *Controller) ValidateOperatorRecovery(allocationID string) error {
	allocationID = strings.TrimSpace(allocationID)
	if h == nil || allocationID == "" {
		return errord.ErrInvalidArgument
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[allocationID]
	if state == nil || state.record == nil || state.record.GetAllocationID() != allocationID {
		return fmt.Errorf("allocation %q has no authoritative AllocationState: %w", allocationID, errord.ErrNotFound)
	}
	if !validStartRequestDigest(state.record.GetAllocationRequestDigest()) || state.runtime == nil {
		return fmt.Errorf("allocation %q runtime identity is incomplete: %w", allocationID, errord.ErrFailedPrecondition)
	}
	return nil
}

// ValidateOperatorInspection proves that a node-local diagnostic request is
// addressing the runtime projection recorded by one AllocationState. A live
// lease is deliberately not required: operators must still be able to inspect
// and wait for an Allocation while it is terminating or partitioned.
func (h *Controller) ValidateOperatorInspection(allocationID string) error {
	return h.ValidateOperatorRecovery(allocationID)
}

// ValidateAllocationNetworkResolution is stricter than operator inspection:
// node-tunneld may resolve only a control-plane-bound, currently executable
// Allocation. Local conformance sessions can never acquire tunnel authority.
func (h *Controller) ValidateAllocationNetworkResolution(allocationID string, now time.Time) error {
	if err := h.ValidateOperatorExecution(allocationID, now); err != nil {
		return err
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record == nil || strings.TrimSpace(state.record.GetNodeID()) == "" {
		return fmt.Errorf("allocation %q is not control-plane-bound: %w", allocationID, errord.ErrFailedPrecondition)
	}
	return nil
}
