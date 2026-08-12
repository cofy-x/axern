package container

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

// ReconcileRuntimeInventory removes persisted container metadata and resource
// claims only when a successful inventory generation proves that the owning
// runtime no longer has the container. Validation is completed before the
// first deletion so missing handlers, missing metadata, or ownership mismatch
// cannot cause partial destructive reconciliation.
func (m *Manager) ReconcileRuntimeInventory(inventory map[string]map[string]struct{}) error {
	if err := m.ValidateRuntimeInventory(inventory); err != nil {
		return err
	}

	staleSet := make(map[string]struct{})
	for id, container := range m.containers.Items() {
		ids := inventory[container.Metadata.GetRuntimeHandler()]
		if _, live := ids[id]; !live {
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
			live := false
			for _, ids := range inventory {
				if _, live = ids[id]; live {
					break
				}
			}
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

// ValidateRuntimeInventory proves that the complete runtime generation can be
// reconciled without crossing an unknown ownership boundary. Callers use this
// before cleaning allocation, storage, or container records.
func (m *Manager) ValidateRuntimeInventory(inventory map[string]map[string]struct{}) error {
	if m == nil {
		return errors.New("container manager is required")
	}
	for runtimeName, ids := range inventory {
		for id := range ids {
			container, ok := m.containers.Get(id)
			if !ok || container == nil || container.Metadata == nil {
				return fmt.Errorf("runtime %s container %s has no persisted metadata", runtimeName, id)
			}
			if container.Metadata.GetRuntimeHandler() != runtimeName {
				return fmt.Errorf("runtime %s container %s is owned by persisted handler %s", runtimeName, id, container.Metadata.GetRuntimeHandler())
			}
		}
	}

	for id, container := range m.containers.Items() {
		if container == nil || container.Metadata == nil {
			return fmt.Errorf("persisted container %s has no metadata", id)
		}
		runtimeName := container.Metadata.GetRuntimeHandler()
		_, runtimeKnown := inventory[runtimeName]
		if !runtimeKnown {
			return fmt.Errorf("persisted container %s is owned by runtime %s whose inventory is unavailable", id, runtimeName)
		}
	}
	return nil
}
