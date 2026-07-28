package storage

import (
	"context"
	"fmt"
	"strings"

	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
)

func (m *Manager) Unpublish(ctx context.Context, allocationID, bindingID string) ([]*runtimevolumev1.PublishedVolume, error) {
	if m == nil {
		return nil, nil
	}
	allocationID = strings.TrimSpace(allocationID)
	bindingID = strings.TrimSpace(bindingID)
	if allocationID == "" {
		return nil, fmt.Errorf("allocation id is required")
	}
	m.mu.Lock()
	items := m.published[allocationID]
	removed := make([]*runtimevolumev1.PublishedVolume, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if bindingID == "" || item.GetBindingID() == bindingID {
			removed = append(removed, clonePublished(item))
		}
	}
	m.mu.Unlock()

	var firstErr error
	for _, item := range removed {
		if err := m.unpublishOne(ctx, allocationID, item); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	var changed bool
	for _, item := range removed {
		if removePublishedIfEqual(m.published, allocationID, item) {
			changed = true
		}
	}
	if !changed {
		return clonePublishedSlice(removed), nil
	}
	if err := m.saveLocked(ctx); err != nil {
		return nil, err
	}
	return clonePublishedSlice(removed), nil
}

func (m *Manager) unpublishOne(ctx context.Context, allocationID string, item *runtimevolumev1.PublishedVolume) error {
	if item == nil {
		return nil
	}
	provider := m.providers[item.GetBackend()]
	if provider == nil {
		return fmt.Errorf("volume backend %s is not supported", item.GetBackend())
	}
	return provider.Unpublish(ctx, allocationID, item)
}
