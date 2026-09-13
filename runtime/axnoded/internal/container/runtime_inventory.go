package container

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

// ReconcileRuntimeInventory removes persisted container metadata and resource
// claims only when a successful inventory generation proves that the owning
// runsc no longer has the container. Validation is completed before the first
// deletion so missing metadata cannot cause partial destructive reconciliation.
func (m *Manager) ReconcileRuntimeInventory(inventory map[string]struct{}) error {
	if err := m.ValidateRuntimeInventory(inventory); err != nil {
		return err
	}

	staleSet := make(map[string]struct{})
	for id := range m.containers.Items() {
		if _, live := inventory[id]; !live {
			staleSet[id] = struct{}{}
		}
	}
	directories, err := os.ReadDir(m.root)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("list persisted container directories: %w", err)
	}
	for _, directory := range directories {
		if directory.IsDir() {
			id := directory.Name()
			_, live := inventory[id]
			if !live {
				staleSet[id] = struct{}{}
			}
		}
	}
	stale := make([]string, 0, len(staleSet))
	for id := range staleSet {
		stale = append(stale, id)
	}
	sort.Strings(stale)

	var result error
	for _, id := range stale {
		if err := m.DeleteAfterConfirmedRuntimeAbsence(id); err != nil {
			result = errors.Join(result, fmt.Errorf("cleanup orphan container %s: %w", id, err))
		}
	}
	return result
}

// ValidateRuntimeInventory proves that the complete runsc inventory can be
// reconciled without crossing an unknown ownership boundary. Callers use this
// before cleaning allocation, storage, or container records.
func (m *Manager) ValidateRuntimeInventory(inventory map[string]struct{}) error {
	if m == nil {
		return errors.New("container manager is required")
	}
	for id := range inventory {
		container, ok := m.containers.Get(id)
		if !ok || container == nil || container.Metadata == nil {
			return fmt.Errorf("runsc container %s has no persisted metadata", id)
		}
	}

	for id, container := range m.containers.Items() {
		if container == nil || container.Metadata == nil {
			return fmt.Errorf("persisted container %s has no metadata", id)
		}
	}
	return nil
}
