package oci

import (
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) ensureChainLowerDirs(chainIDs []string, layerPaths []string) ([]string, error) {
	if len(chainIDs) != len(layerPaths) {
		return nil, fmt.Errorf("chain count mismatch: chainIDs=%d paths=%d", len(chainIDs), len(layerPaths))
	}

	paths := make([]string, len(chainIDs))
	reservedChainIDs := make([]string, 0, len(chainIDs))
	for i, chainID := range chainIDs {
		if chainID == "" {
			m.rollbackReservedChainRefs(reservedChainIDs)
			return nil, fmt.Errorf("chainID at index %d is empty", i)
		}
		if layerPaths[i] == "" {
			m.rollbackReservedChainRefs(reservedChainIDs)
			return nil, fmt.Errorf("layer path at index %d is empty", i)
		}
		path, err := m.getOrCreateChainLowerDir(chainID, layerPaths[i])
		if err != nil {
			m.rollbackReservedChainRefs(reservedChainIDs)
			return nil, err
		}
		paths[i] = path
		reservedChainIDs = append(reservedChainIDs, chainID)
	}
	return paths, nil
}

func (m *Manager) getOrCreateChainLowerDir(chainID string, layerPath string) (string, error) {
	unlock := m.acquireChainLock(chainID)
	defer unlock()

	record, err := m.store.getChain(chainID)
	if err != nil {
		return "", fmt.Errorf("failed to query chain metadata %s: %w", chainID, err)
	}
	if record != nil && record.Path != "" && pathExists(record.Path) {
		record, err = m.store.incrementChainRef(chainID, m.now().Unix())
		if err != nil {
			return "", fmt.Errorf("failed to reserve chain metadata %s: %w", chainID, err)
		}
		return record.Path, nil
	}

	chainDir, err := m.store.getOrCreateChainDir(chainID)
	if err != nil {
		return "", fmt.Errorf("failed to allocate lowerdir for chain %s: %w", chainID, err)
	}
	targetPath := filepath.Join(m.chainsDir, chainDir, "fs")
	if pathExists(targetPath) {
		recoveredRefCount := 1
		if record == nil {
			record = &ChainRecord{ChainID: chainID}
		} else {
			recoveredRefCount = record.RefCount + 1
		}
		record.Path = targetPath
		record.RefCount = recoveredRefCount
		record.RefZeroAtUnix = 0
		record.LastUsedUnix = m.now().Unix()
		if err := m.store.putChain(record); err != nil {
			return "", fmt.Errorf("failed to persist recovered chain metadata %s: %w", chainID, err)
		}
		return targetPath, nil
	}

	if err := m.materializeChainLowerDir(layerPath, targetPath); err != nil {
		return "", fmt.Errorf("failed to build lowerdir for chain %s from %s: %w", chainID, layerPath, err)
	}

	if record == nil {
		record = &ChainRecord{
			ChainID:       chainID,
			Path:          targetPath,
			RefCount:      1,
			RefZeroAtUnix: 0,
			LastUsedUnix:  m.now().Unix(),
		}
	} else {
		record.Path = targetPath
		record.RefCount++
		record.LastUsedUnix = m.now().Unix()
		record.RefZeroAtUnix = 0
	}
	if err := m.store.putChain(record); err != nil {
		return "", fmt.Errorf("failed to persist chain metadata %s: %w", chainID, err)
	}
	return targetPath, nil
}

func (m *Manager) materializeChainLowerDir(sourcePath string, targetPath string) error {
	parentDir := filepath.Dir(targetPath)
	tmpDir := filepath.Join(parentDir, fmt.Sprintf("tmp-%d", m.now().UnixNano()))

	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create chain lowerdir parent %s: %w", parentDir, err)
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("failed to cleanup chain lowerdir temp dir %s: %w", tmpDir, err)
	}
	if err := buildHardlinkTree(sourcePath, tmpDir); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Errorf("failed to cleanup chain lowerdir target %s: %w", targetPath, err)
	}
	if err := os.Rename(tmpDir, targetPath); err != nil {
		return fmt.Errorf("failed to place chain lowerdir at %s: %w", targetPath, err)
	}
	return nil
}
