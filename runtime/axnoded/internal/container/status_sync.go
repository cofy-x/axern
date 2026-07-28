package container

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (m *Manager) SyncStatusFromState(id string, state *contract.UnionContainerState) error {
	container, ok := m.containers.Get(id)
	if !ok || container == nil {
		return errord.ErrNotFound
	}
	if state == nil {
		return nil
	}
	if container.Status == nil {
		container.Status = GenerateStatusFromState(state, container.PATH)
		return nil
	}
	return container.Status.UpdateSync(func(status Status) (Status, error) {
		return UpdateStatusByState(state, status), nil
	})
}
