package oci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func (m *Manager) rollbackMountTransaction(txn *OciMountTxnRecord, incrementedDigests []string, incrementedChains []string) {
	if txn != nil {
		if err := m.unmountFn(txn.MountPath); err != nil && !isNotMountedError(err) {
			logrus.Warnf("failed to rollback mount %s: %v", txn.MountPath, err)
		}
	}

	m.rollbackReservedLayerRefs(incrementedDigests)
	m.rollbackReservedChainRefs(incrementedChains)

	if txn != nil {
		if err := m.store.deleteMountTxn(txn.mountKey()); err != nil {
			logrus.Warnf("failed to delete rollback mount transaction for %s: %v", txn.ImageURL, err)
		}
		_ = os.RemoveAll(filepath.Dir(txn.MountPath))
	}
}

func (m *Manager) rollbackReservedLayerRefs(digests []string) {
	for _, digest := range digests {
		if digest == "" {
			continue
		}
		if _, err := m.store.decrementLayerRef(digest, m.now().Unix()); err != nil && !errors.Is(err, ErrLayerNotFound) {
			logrus.Warnf("failed to rollback refcount for layer %s: %v", digest, err)
		}
	}
}

func (m *Manager) rollbackReservedChainRefs(chainIDs []string) {
	for _, chainID := range chainIDs {
		if chainID == "" {
			continue
		}
		if _, err := m.store.decrementChainRef(chainID, m.now().Unix()); err != nil && !errors.Is(err, ErrChainNotFound) {
			logrus.Warnf("failed to rollback refcount for chain %s: %v", chainID, err)
		}
	}
}

func (m *Manager) recoverMountTransactions(mountSnapshot managedMountSnapshot) error {
	txns, err := m.store.listMountTxns()
	if err != nil {
		return fmt.Errorf("failed to list mount transactions: %w", err)
	}

	for _, txn := range txns {
		if txn.ImageURL == "" || txn.MountPath == "" || len(txn.LayerDigests) == 0 {
			_ = m.store.deleteMountTxn(txn.mountKey())
			continue
		}

		if rec, err := m.store.getMount(txn.mountKey()); err == nil && rec != nil {
			_ = m.store.deleteMountTxn(txn.mountKey())
			continue
		}

		if mountSnapshot.authoritative {
			if _, ok := mountSnapshot.paths[txn.MountPath]; ok {
				validLayers, err := m.validateLayerDigests(txn.LayerDigests)
				if err != nil {
					return fmt.Errorf("failed to validate mount transaction layers for %s: %w", txn.ImageURL, err)
				}
				if !validLayers {
					logrus.Warnf("drop mount transaction for %s: missing layer metadata/path", txn.ImageURL)
					_ = m.store.deleteMountTxn(txn.mountKey())
					continue
				}
				if len(txn.ChainIDs) > 0 {
					validChains, err := m.validateChainIDs(txn.ChainIDs)
					if err != nil {
						return fmt.Errorf("failed to validate mount transaction chains for %s: %w", txn.ImageURL, err)
					}
					if !validChains {
						logrus.Warnf("drop mount transaction for %s: missing chain metadata/path", txn.ImageURL)
						_ = m.store.deleteMountTxn(txn.mountKey())
						continue
					}
				}
				rec := &OciMountRecord{
					CacheKey:      txn.CacheKey,
					ImageURL:      txn.ImageURL,
					MountID:       txn.MountID,
					MountPath:     txn.MountPath,
					LayerDigests:  append([]string(nil), txn.LayerDigests...),
					ChainIDs:      append([]string(nil), txn.ChainIDs...),
					LowerDirs:     append([]string(nil), txn.LowerDirs...),
					CreatedAtUnix: txn.CreatedAtUnix,
				}
				if err := m.store.putMount(rec); err != nil {
					return fmt.Errorf("failed to recover mount metadata for %s: %w", txn.ImageURL, err)
				}
			}
		}

		_ = m.store.deleteMountTxn(txn.mountKey())
	}

	return nil
}

func (m *Manager) validateLayerDigests(digests []string) (bool, error) {
	for _, digest := range digests {
		layer, err := m.store.getLayer(digest)
		if err != nil {
			return false, err
		}
		if layer == nil || layer.Path == "" || !pathExists(layer.Path) {
			return false, nil
		}
	}
	return true, nil
}

func (m *Manager) validateChainIDs(chainIDs []string) (bool, error) {
	for _, chainID := range chainIDs {
		chain, err := m.store.getChain(chainID)
		if err != nil {
			return false, err
		}
		if chain == nil || chain.Path == "" || !pathExists(chain.Path) {
			return false, nil
		}
	}
	return true, nil
}
