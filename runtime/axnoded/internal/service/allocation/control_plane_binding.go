package allocation

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"google.golang.org/protobuf/proto"
)

// BindControlPlaneAllocation persists the one relation that authorizes node
// observations for an Allocation to be sent to controld. AllocationState and
// runtime metadata remain recovery facts and cannot imply this binding.
func (h *Controller) BindControlPlaneAllocation(allocationID, nodeID, requestDigest string) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	requestDigest = strings.TrimSpace(requestDigest)
	if allocationID == "" || nodeID == "" || !validStartRequestDigest(requestDigest) {
		return errors.New("control-plane allocation binding requires allocation id, node id, and canonical request digest")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	desired := &apipb.ControlPlaneAllocationBinding{
		AllocationID:  allocationID,
		NodeID:        nodeID,
		RequestDigest: requestDigest,
	}
	h.stateMu.RLock()
	current := h.controlPlaneBindings[allocationID]
	h.stateMu.RUnlock()
	if current != nil {
		if proto.Equal(current, desired) {
			return nil
		}
		return fmt.Errorf("control-plane allocation binding %q conflicts with the durable admission", allocationID)
	}
	if err := h.store.PutRecord(config.ControlPlaneAllocationBindingBucket, allocationID, desired); err != nil {
		return fmt.Errorf("persist control-plane allocation binding %q: %w", allocationID, err)
	}
	h.stateMu.Lock()
	h.controlPlaneBindings[allocationID] = desired
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) ReleaseControlPlaneAllocation(allocationID, nodeID string) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	if allocationID == "" {
		return nil
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	current := h.controlPlaneBindings[allocationID]
	h.stateMu.RUnlock()
	if current != nil && nodeID != "" && current.GetNodeID() != nodeID {
		return fmt.Errorf("control-plane allocation binding %q belongs to node %q", allocationID, current.GetNodeID())
	}
	if err := h.store.DeleteRecord(config.ControlPlaneAllocationBindingBucket, allocationID); err != nil {
		return fmt.Errorf("delete control-plane allocation binding %q: %w", allocationID, err)
	}
	h.stateMu.Lock()
	delete(h.controlPlaneBindings, allocationID)
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) HasControlPlaneBinding(allocationID string) bool {
	if h == nil {
		return false
	}
	allocationID = strings.TrimSpace(allocationID)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	binding := h.controlPlaneBindings[allocationID]
	return binding != nil && binding.GetAllocationID() == allocationID
}

func (h *Controller) ControlPlaneBindingMatches(allocationID, nodeID string) bool {
	if h == nil {
		return false
	}
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	binding := h.controlPlaneBindings[allocationID]
	return binding != nil && binding.GetAllocationID() == allocationID && binding.GetNodeID() == nodeID
}

// ControlPlaneAllocationIDs returns only bound Allocations that also have an
// active node recovery record. A binding written immediately before Create is
// not itself evidence that a runtime execution exists.
func (h *Controller) ControlPlaneAllocationIDs() []string {
	if h == nil {
		return nil
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	ids := make([]string, 0, len(h.controlPlaneBindings))
	for allocationID, binding := range h.controlPlaneBindings {
		state := h.allocationStates[allocationID]
		if binding != nil && binding.GetAllocationID() == allocationID && state != nil && state.record != nil && state.record.GetAllocationID() == allocationID {
			ids = append(ids, allocationID)
		}
	}
	sort.Strings(ids)
	return ids
}

func (h *Controller) loadControlPlaneBindings() (map[string]struct{}, error) {
	bindings := make(map[string]*apipb.ControlPlaneAllocationBinding)
	if h == nil || h.store == nil {
		return map[string]struct{}{}, nil
	}
	err := h.store.ForEachRecord(config.ControlPlaneAllocationBindingBucket, func(key string, value []byte) error {
		binding := new(apipb.ControlPlaneAllocationBinding)
		if err := proto.Unmarshal(value, binding); err != nil {
			return fmt.Errorf("decode control-plane allocation binding %s: %w", key, err)
		}
		if strings.TrimSpace(key) == "" || binding.GetAllocationID() != key || strings.TrimSpace(binding.GetNodeID()) == "" || !validStartRequestDigest(binding.GetRequestDigest()) {
			return fmt.Errorf("control-plane allocation binding %s is invalid", key)
		}
		bindings[key] = binding
		return nil
	})
	if err != nil {
		return nil, err
	}
	h.stateMu.Lock()
	h.controlPlaneBindings = bindings
	h.stateMu.Unlock()
	ids := make(map[string]struct{}, len(bindings))
	for id := range bindings {
		ids[id] = struct{}{}
	}
	return ids, nil
}

func (h *Controller) RestoreControlPlaneBindings() (map[string]struct{}, error) {
	return h.loadControlPlaneBindings()
}
