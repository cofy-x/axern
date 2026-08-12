package container

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

// SyncRuntimeIdentityFromState enriches a newly created, locally nonterminal
// container only from a runtime state that is still running. An exited list
// entry cannot safely publish an init PID because the host may already have
// reused it. Runtime list output is not authoritative exit evidence: the Wait
// observer may already have persisted an exact terminal status by the time
// create-time synchronization runs.
func (m *Manager) SyncRuntimeIdentityFromState(id string, state *contract.UnionContainerState) error {
	container, ok := m.containers.Get(id)
	if !ok || container == nil || container.Status == nil {
		return errord.ErrNotFound
	}
	if state == nil {
		return nil
	}
	return container.Status.UpdateSync(func(status Status) (Status, error) {
		if status.FinishedAt != "" || state.Status != contract.ContainerStatusRunning {
			return status, nil
		}
		if state.InitProcessPid > 0 {
			status.Pid = state.InitProcessPid
		}
		if state.Created != "" {
			status.StartedAt = state.Created
		}
		return status, nil
	})
}
