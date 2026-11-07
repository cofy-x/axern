package oci

import (
	"fmt"
	"os"
	"path/filepath"
)

func (m *Manager) ensureChainLowerDirs(chainIDs []string, layers []*LayerRecord) ([]string, error) {
	if len(chainIDs) != len(layers) {
		return nil, fmt.Errorf("chain count mismatch: chainIDs=%d layers=%d", len(chainIDs), len(layers))
	}

	paths := make([]string, len(chainIDs))
	reservedChainIDs := make([]string, 0, len(chainIDs))
	for i, chainID := range chainIDs {
		if chainID == "" {
			m.rollbackReservedChainRefs(reservedChainIDs)
			return nil, fmt.Errorf("chainID at index %d is empty", i)
		}
		if layers[i] == nil || layers[i].Path == "" {
			m.rollbackReservedChainRefs(reservedChainIDs)
			return nil, fmt.Errorf("layer path at index %d is empty", i)
		}
		path, err := m.getOrCreateChainLowerDir(chainID, layers[i], paths[:i])
		if err != nil {
			m.rollbackReservedChainRefs(reservedChainIDs)
			return nil, err
		}
		paths[i] = path
		reservedChainIDs = append(reservedChainIDs, chainID)
	}
	return paths, nil
}

func (m *Manager) getOrCreateChainLowerDir(chainID string, layer *LayerRecord, previousRoots []string) (string, error) {
	unlock := m.acquireChainLock(chainID)
	defer unlock()

	record, err := m.store.getChain(chainID)
	if err != nil {
		return "", fmt.Errorf("failed to query chain metadata %s: %w", chainID, err)
	}
	chainDir, err := m.store.getOrCreateChainDir(chainID)
	if err != nil {
		return "", fmt.Errorf("derive lowerdir for chain %s: %w", chainID, err)
	}
	targetPath := filepath.Join(m.chainsDir, chainDir, "fs")
	if record != nil && record.Path != "" && filepath.Clean(record.Path) != filepath.Clean(targetPath) {
		return "", fmt.Errorf("chain metadata path %q differs from content path %q", record.Path, targetPath)
	}
	if record != nil && record.Path != "" && pathExists(record.Path) {
		record, err = m.store.incrementChainRef(chainID, m.now().Unix())
		if err != nil {
			return "", fmt.Errorf("failed to reserve chain metadata %s: %w", chainID, err)
		}
		return record.Path, nil
	}

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

	if err := m.materializeChainLowerDir(layer, targetPath, previousRoots); err != nil {
		return "", fmt.Errorf("failed to build lowerdir for chain %s from %s: %w", chainID, layer.Path, err)
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

func (m *Manager) materializeChainLowerDir(layer *LayerRecord, targetPath string, previousRoots []string) error {
	parentDir := filepath.Dir(targetPath)
	tmpDir := filepath.Join(parentDir, fmt.Sprintf("tmp-%d", m.now().UnixNano()))

	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return fmt.Errorf("failed to create chain lowerdir parent %s: %w", parentDir, err)
	}
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("failed to cleanup chain lowerdir temp dir %s: %w", tmpDir, err)
	}
	if err := buildHardlinkTree(layer.Path, tmpDir); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := inheritImplicitDirectoryMetadata(tmpDir, previousRoots, layer.ImplicitDirectories, overlayAttribute); err != nil {
		return err
	}
	rootMetadataPath := layer.Path
	if !layer.HasRootMetadata && len(previousRoots) > 0 {
		rootMetadataPath = previousRoots[len(previousRoots)-1]
	}
	rootInfo, err := os.Lstat(rootMetadataPath)
	if err != nil {
		return fmt.Errorf("stat effective chain root metadata: %w", err)
	}
	if !rootInfo.IsDir() {
		return fmt.Errorf("effective chain root metadata is not a directory")
	}
	// Root ownership/mode inherit when absent from a layer; overlay whiteout
	// xattrs remain those of this layer and must not inherit from its parent.
	if err := copyDirectoryStatMetadata(tmpDir, rootInfo); err != nil {
		return err
	}

	if err := os.RemoveAll(targetPath); err != nil {
		return fmt.Errorf("failed to cleanup chain lowerdir target %s: %w", targetPath, err)
	}
	if err := os.Rename(tmpDir, targetPath); err != nil {
		return fmt.Errorf("failed to place chain lowerdir at %s: %w", targetPath, err)
	}
	return nil
}
