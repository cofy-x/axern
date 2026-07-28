package storage

import (
	"context"
	"fmt"
	"strings"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func (m *Manager) Delete(ctx context.Context, claimID string, backend storagev1.VolumeBackend, backendHandle string) error {
	if m == nil {
		return fmt.Errorf("volume manager is required")
	}
	claimID = strings.TrimSpace(claimID)
	backendHandle = strings.TrimSpace(backendHandle)
	if claimID == "" || backendHandle == "" {
		return fmt.Errorf("claim id and backend handle are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, volumes := range m.published {
		for _, volume := range volumes {
			if volume != nil && volume.GetClaimID() == claimID {
				return fmt.Errorf("volume claim %q is still published", claimID)
			}
		}
	}
	provider := m.providers[backend]
	if provider == nil {
		return fmt.Errorf("volume backend %s is not supported", backend)
	}
	if err := provider.Delete(ctx, claimID, backendHandle); err != nil {
		return err
	}
	return m.saveLocked(ctx)
}
