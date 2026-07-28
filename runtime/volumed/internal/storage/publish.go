package storage

import (
	"context"
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func (m *Manager) Publish(ctx context.Context, allocationID, runtimeClass string, volume *privatestoragev1.ResolvedNodeVolume) (*runtimevolumev1.PublishedVolume, error) {
	if m == nil {
		return nil, fmt.Errorf("volume manager is not configured")
	}
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil, fmt.Errorf("allocation id is required")
	}
	if volume == nil {
		return nil, fmt.Errorf("resolved node volume is required")
	}
	if err := validateRuntimeCompatibility(runtimeClass, volume.GetRuntimeCompatibility()); err != nil {
		return nil, err
	}
	provider := m.providers[volume.GetBackend()]
	if provider == nil {
		return nil, fmt.Errorf("volume backend %s is not supported", volume.GetBackend())
	}
	if err := validateVolumeAgainstProvider(provider, runtimeClass, volume); err != nil {
		return nil, err
	}
	item, err := provider.Publish(ctx, allocationID, volume)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, fmt.Errorf("volume backend %s returned an empty published volume", volume.GetBackend())
	}
	if item.GetBackend() == storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
		item.Backend = provider.Backend()
	}
	if item.GetBindingID() == "" {
		item.BindingID = volume.GetBindingID()
	}
	if item.GetClaimID() == "" {
		item.ClaimID = volume.GetClaimID()
	}
	if item.GetBackend() != provider.Backend() {
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, fmt.Errorf("volume backend %s returned published backend %s", provider.Backend(), item.GetBackend())
	}
	if item.GetBindingID() != volume.GetBindingID() {
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, fmt.Errorf("volume backend %s returned binding id %q, want %q", provider.Backend(), item.GetBindingID(), volume.GetBindingID())
	}
	if item.GetClaimID() != volume.GetClaimID() {
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, fmt.Errorf("volume backend %s returned claim id %q, want %q", provider.Backend(), item.GetClaimID(), volume.GetClaimID())
	}
	if item.GetReadonly() != volume.GetReadonly() {
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, fmt.Errorf("volume backend %s returned readonly %v, want %v", provider.Backend(), item.GetReadonly(), volume.GetReadonly())
	}
	target := cleanContainerTarget(volume.GetTarget())
	if target == "" || cleanContainerTarget(item.GetTarget()) != target {
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, fmt.Errorf("volume backend %s returned target %q, want %q", provider.Backend(), item.GetTarget(), target)
	}
	if err := validatePublishedRecord(allocationID, item); err != nil {
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	upsertPublished(m.published, allocationID, item)
	if err := m.saveLocked(ctx); err != nil {
		removePublished(m.published, allocationID, item.GetBindingID())
		_ = provider.Unpublish(ctx, allocationID, item)
		return nil, err
	}
	return clonePublished(item), nil
}

func validateVolumeAgainstProvider(provider Provider, runtimeClass string, volume *privatestoragev1.ResolvedNodeVolume) error {
	capabilities := provider.Capabilities()
	if !providerSupportsAccessMode(capabilities, volume.GetAccessMode()) {
		return fmt.Errorf("volume backend %s does not support access mode %s", provider.Backend(), volume.GetAccessMode())
	}
	if !providerSupportsConsistencyProfile(capabilities, volume.GetConsistencyProfile()) {
		return fmt.Errorf("volume backend %s does not support consistency profile %s", provider.Backend(), volume.GetConsistencyProfile())
	}
	if err := validateRuntimeCompatibility(runtimeClass, capabilities.RuntimeCompatibility); err != nil {
		return fmt.Errorf("volume backend %s provider compatibility: %w", provider.Backend(), err)
	}
	return nil
}
