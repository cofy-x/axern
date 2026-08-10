package api

import (
	"strings"
	"sync"
)

type AllocationTargetRegistry struct {
	mu      sync.RWMutex
	targets map[string]string
	deleted map[string]struct{}
}

func NewAllocationTargetRegistry() *AllocationTargetRegistry {
	return &AllocationTargetRegistry{
		targets: make(map[string]string),
		deleted: make(map[string]struct{}),
	}
}

func (r *AllocationTargetRegistry) bind(allocationID, targetID string) {
	if r == nil {
		return
	}
	allocationID = strings.TrimSpace(allocationID)
	targetID = strings.TrimSpace(targetID)
	if allocationID == "" || targetID == "" {
		return
	}
	r.mu.Lock()
	delete(r.deleted, allocationID)
	if allocationID == targetID {
		delete(r.targets, allocationID)
	} else {
		r.targets[allocationID] = targetID
	}
	r.mu.Unlock()
}

func (r *AllocationTargetRegistry) resolve(allocationID string) string {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || r == nil {
		return allocationID
	}
	r.mu.RLock()
	targetID := r.targets[allocationID]
	r.mu.RUnlock()
	if strings.TrimSpace(targetID) == "" {
		return allocationID
	}
	return targetID
}

func (r *AllocationTargetRegistry) unbind(allocationID string) {
	if r == nil {
		return
	}
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return
	}
	r.mu.Lock()
	delete(r.targets, allocationID)
	r.mu.Unlock()
}

func (r *AllocationTargetRegistry) markDeleted(allocationID string) {
	if r == nil {
		return
	}
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return
	}
	r.mu.Lock()
	delete(r.targets, allocationID)
	r.deleted[allocationID] = struct{}{}
	r.mu.Unlock()
}

func (r *AllocationTargetRegistry) isDeleted(allocationID string) bool {
	if r == nil {
		return false
	}
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return false
	}
	r.mu.RLock()
	_, ok := r.deleted[allocationID]
	r.mu.RUnlock()
	return ok
}
