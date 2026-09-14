package allocation

import (
	"sort"
	"strings"
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
