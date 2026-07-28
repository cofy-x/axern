package ossloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entries, err := os.ReadDir(m.recordsDir)
	if err != nil {
		return fmt.Errorf("failed to read oss loop records: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		recordPath := filepath.Join(m.recordsDir, entry.Name())
		record, err := readRecord(recordPath)
		if err != nil {
			return fmt.Errorf("failed to load record %s: %w", recordPath, err)
		}
		if err := validateMountID(record.ID); err != nil {
			return fmt.Errorf("invalid oss loop record %s: %w", recordPath, err)
		}
		mounted, err := m.mountedFn(record.MountPath)
		if err != nil {
			return fmt.Errorf("failed to inspect mount path %s: %w", record.MountPath, err)
		}
		if !mounted {
			_ = os.Remove(recordPath)
			_ = os.RemoveAll(m.lowerPath(record.ID))
			_ = os.RemoveAll(record.MountPath)
			continue
		}
		m.states[record.ID] = &mountState{record: *record}
	}
	return nil
}

func (m *Manager) loadStateLocked(id string) (*mountState, error) {
	if state, ok := m.states[id]; ok {
		return state, nil
	}

	recordPath := m.recordPath(id)
	record, err := readRecord(recordPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read record for %s: %w", id, err)
	}

	mounted, err := m.mountedFn(record.MountPath)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect mount path %s: %w", record.MountPath, err)
	}
	if !mounted {
		_ = os.Remove(recordPath)
		_ = os.RemoveAll(m.lowerPath(id))
		_ = os.RemoveAll(record.MountPath)
		return nil, nil
	}

	state := &mountState{record: *record}
	m.states[id] = state
	return state, nil
}

func (m *Manager) writeRecordLocked(record *Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal loop mount record: %w", err)
	}
	if err := os.WriteFile(m.recordPath(record.ID), data, 0600); err != nil {
		return fmt.Errorf("failed to write loop mount record: %w", err)
	}
	return nil
}

func (m *Manager) deleteRecordLocked(id string) error {
	if err := os.Remove(m.recordPath(id)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove loop mount record %s: %w", id, err)
	}
	return nil
}

func (m *Manager) unmountIfMountedLocked(id, targetPath string) error {
	mounted, err := m.mountedFn(targetPath)
	if err != nil {
		return fmt.Errorf("failed to inspect mount path %s: %w", targetPath, err)
	}
	if !mounted {
		return nil
	}
	if err := m.unmountFn(m.lowerPath(id), targetPath); err != nil {
		return fmt.Errorf("failed to unmount loop mount %s: %w", targetPath, err)
	}
	return nil
}

func (m *Manager) recordPath(id string) string {
	return filepath.Join(m.recordsDir, id+".json")
}

func readRecord(path string) (*Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	record := &Record{}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal record: %w", err)
	}
	return record, nil
}
