package oci

import (
	"fmt"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func (m *Manager) reconcileState() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.containers = make(map[string]*ContainerInfo)

	m.cleanupStaleLayerTempDirs()
	m.cleanupStaleChainTempDirs()

	layers, err := m.store.listLayers()
	if err != nil {
		return fmt.Errorf("failed to list layers: %w", err)
	}
	layerMap := make(map[string]*LayerRecord, len(layers))
	for _, layer := range layers {
		if layer.Path == "" || !pathExists(layer.Path) {
			if err := m.store.deleteLayer(layer.Digest); err != nil {
				return fmt.Errorf("failed to delete stale layer %s: %w", layer.Digest, err)
			}
			continue
		}
		layerMap[layer.Digest] = layer
	}

	chains, err := m.store.listChains()
	if err != nil {
		return fmt.Errorf("failed to list chains: %w", err)
	}
	chainMap := make(map[string]*ChainRecord, len(chains))
	validChainPaths := make(map[string]struct{}, len(chains))
	for _, chain := range chains {
		if chain.Path == "" || !pathExists(chain.Path) {
			if err := m.store.deleteChain(chain.ChainID); err != nil {
				return fmt.Errorf("failed to delete stale chain %s: %w", chain.ChainID, err)
			}
			continue
		}
		chainMap[chain.ChainID] = chain
		validChainPaths[filepath.Clean(chain.Path)] = struct{}{}
	}
	m.cleanupOrphanChainDirs(validChainPaths)

	mountSnapshot, err := m.readMnts()
	if err != nil {
		return fmt.Errorf("failed to read /proc/mounts: %w", err)
	}

	if err := m.recoverMountTransactions(mountSnapshot); err != nil {
		return err
	}

	mounts, err := m.store.listMounts()
	if err != nil {
		return fmt.Errorf("failed to list mounts: %w", err)
	}

	refCounts := make(map[string]int)
	chainRefCounts := make(map[string]int)
	validMountPoints := make(map[string]struct{}, len(mounts))
	for _, mount := range mounts {
		if mount.ImageURL == "" || mount.MountPath == "" || len(mount.LayerDigests) == 0 {
			_ = m.store.deleteMount(mount.mountKey())
			continue
		}
		if mountSnapshot.authoritative {
			if _, ok := mountSnapshot.paths[mount.MountPath]; !ok {
				_ = m.store.deleteMount(mount.mountKey())
				continue
			}
		}

		valid := true
		for _, digest := range mount.LayerDigests {
			layer, ok := layerMap[digest]
			if !ok || layer.Path == "" || !pathExists(layer.Path) {
				valid = false
				break
			}
		}
		if !valid {
			_ = m.store.deleteMount(mount.mountKey())
			continue
		}
		if len(mount.ChainIDs) > 0 {
			validChains, err := m.validateChainIDs(mount.ChainIDs)
			if err != nil {
				return fmt.Errorf("failed to validate chain metadata for %s: %w", mount.ImageURL, err)
			}
			if !validChains {
				_ = m.store.deleteMount(mount.mountKey())
				continue
			}
		}

		for _, digest := range mount.LayerDigests {
			refCounts[digest]++
		}
		for _, chainID := range mount.ChainIDs {
			chainRefCounts[chainID]++
		}
		validMountPoints[filepath.Clean(mount.MountPath)] = struct{}{}
		m.containers[mount.mountKey()] = &ContainerInfo{
			MountID:      mount.MountID,
			ImageURL:     mount.ImageURL,
			MountPath:    mount.MountPath,
			LayerDigests: append([]string(nil), mount.LayerDigests...),
			ChainIDs:     append([]string(nil), mount.ChainIDs...),
			LowerDirs:    append([]string(nil), mount.LowerDirs...),
		}
	}

	if mountSnapshot.authoritative {
		m.cleanupOrphanManagedMounts(mountSnapshot.paths, validMountPoints)
	}

	for digest, layer := range layerMap {
		want := refCounts[digest]
		if layer.RefCount != want {
			layer.RefCount = want
			layer.LastUsedUnix = m.now().Unix()
			if want == 0 {
				if layer.RefZeroAtUnix == 0 {
					layer.RefZeroAtUnix = m.now().Unix()
				}
			} else {
				layer.RefZeroAtUnix = 0
			}
			if err := m.store.putLayer(layer); err != nil {
				return fmt.Errorf("failed to fix layer refcount for %s: %w", digest, err)
			}
		}
	}
	for chainID, chain := range chainMap {
		want := chainRefCounts[chainID]
		if chain.RefCount != want {
			chain.RefCount = want
			chain.LastUsedUnix = m.now().Unix()
			if want == 0 {
				if chain.RefZeroAtUnix == 0 {
					chain.RefZeroAtUnix = m.now().Unix()
				}
			} else {
				chain.RefZeroAtUnix = 0
			}
			if err := m.store.putChain(chain); err != nil {
				return fmt.Errorf("failed to fix chain refcount for %s: %w", chainID, err)
			}
		}
	}

	logrus.WithField("mounts", len(m.containers)).Debug("reconciled OCI state")
	return nil
}
