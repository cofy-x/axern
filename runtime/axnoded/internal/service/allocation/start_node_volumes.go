package allocation

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	servicevolumes "github.com/cofy-x/axern/runtime/axnoded/internal/service/volumes"
)

func (h *Controller) ActiveAllocationIDs() []string {
	if h == nil {
		return nil
	}
	ids := make([]string, 0)
	if h.containers() != nil {
		ids = append(ids, activeAllocationIDs(h.containers().List())...)
	}
	h.stateMu.RLock()
	for id := range h.allocationStates {
		ids = append(ids, id)
	}
	h.stateMu.RUnlock()
	return servicevolumes.NormalizeAllocationIDs(ids)
}

func activeAllocationIDs(containers []*container.Container) []string {
	if len(containers) == 0 {
		return nil
	}
	ids := make([]string, 0, len(containers))
	for _, item := range containers {
		if item == nil || item.Metadata == nil {
			continue
		}
		ids = append(ids, item.Metadata.ID)
	}
	return servicevolumes.NormalizeAllocationIDs(ids)
}
