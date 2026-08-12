package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func (m *Manager) CleanContainerRoot(id string) error {
	path := filepath.Join(m.root, id)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove container root %s: %w", path, err)
	}
	return nil
}

func (m *Manager) Delete(id string) error {
	resource, err := m.CollectResourceByID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !m.containers.Has(id) {
			if err := m.CleanContainerRoot(id); err != nil {
				return err
			}
			if m.idGenerator != nil {
				m.idGenerator.ReleaseId(id)
			}
			return nil
		}
		return fmt.Errorf("collect resource for %s: %w", id, err)
	}
	return m.DeleteWithResource(id, resource)
}

// DeleteAfterConfirmedRuntimeAbsence is used only after a complete runtime
// inventory or an explicit force-delete has proven that no process remains.
// It keeps the exceptional proof path separate from ordinary allocation
// deletion, which must cross the monitor exit-state barrier.
func (m *Manager) DeleteAfterConfirmedRuntimeAbsence(id string) error {
	resource, err := m.CollectResourceByID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !m.containers.Has(id) {
			if err := m.CleanContainerRoot(id); err != nil {
				return err
			}
			if m.idGenerator != nil {
				m.idGenerator.ReleaseId(id)
			}
			return nil
		}
		return fmt.Errorf("collect resource for %s after confirmed runtime absence: %w", id, err)
	}
	return m.DeleteAfterConfirmedRuntimeDelete(id, resource)
}

// DeleteWithResource consumes a claim captured before runtime-owned bundle
// cleanup. The caller can therefore defer the irreversible retiring transition
// until all higher-level storage and lease cleanup has completed.
func (m *Manager) DeleteWithResource(id string, resource OccupiedResource) error {
	if resource.ID == "" {
		resource.ID = id
	}
	// Runtime deletion must have produced and durably stored the terminal exit
	// observation before storage cleanup and cgroup retirement can begin. If the
	// runtime cannot prove an exit, retain every allocation-owned resource as
	// cleanup debt instead of releasing memory commitment optimistically.
	if err := m.waitMonitorExitBarrier(id, 30*time.Second); err != nil {
		return err
	}
	return m.finalizeDeletedContainerResources(id, resource)
}

// DeleteAfterConfirmedRuntimeDelete is restricted to exceptional cleanup after
// a complete inventory or the runtime handler has synchronously confirmed
// Delete (or NotFound). Normal allocation deletion must use DeleteWithResource
// and cross the monitor barrier. When metadata is already durable, checkpoint a
// terminal state before releasing any claim; when metadata never became
// durable, a crash before release leaves the resource-manager lease intact for
// reconciliation.
func (m *Manager) DeleteAfterConfirmedRuntimeDelete(id string, resource OccupiedResource) error {
	if resource.ID == "" {
		resource.ID = id
	}
	m.monitorMu.Lock()
	_, monitored := m.monitors.Get(id)
	m.monitorMu.Unlock()
	if monitored {
		if err := m.waitMonitorExitBarrier(id, 30*time.Second); err != nil {
			return err
		}
	} else if container, ok := m.containers.Get(id); ok && container != nil {
		if container.Status == nil {
			return fmt.Errorf("container %s has no durable status for confirmed runtime deletion", id)
		}
		if container.Status.Get().State() != apipb.ContainerState_CONTAINER_EXITED {
			if err := m.SetExit(id, -1, false, time.Now().UTC(), "runtime deletion confirmed during failed-create rollback", commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED); err != nil {
				return fmt.Errorf("checkpoint confirmed runtime deletion for %s: %w", id, err)
			}
		}
	}
	return m.finalizeDeletedContainerResources(id, resource)
}

func (m *Manager) finalizeDeletedContainerResources(id string, resource OccupiedResource) error {
	if err := m.Release(resource); err != nil {
		return fmt.Errorf("release resources for %s: %w", id, err)
	}
	if err := m.CleanContainerRoot(id); err != nil {
		return err
	}
	if !m.containers.Has(id) {
		return nil
	}
	m.containers.Remove(id)
	return nil
}
