package oci

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

func (m *Manager) cleanupOrphanManagedMounts(managedMounts map[string]struct{}, validMountPoints map[string]struct{}) {
	mountsRoot := filepath.Clean(m.mountsDir) + string(os.PathSeparator)
	for mountPoint := range managedMounts {
		cleanMP := filepath.Clean(mountPoint)
		if !strings.HasPrefix(cleanMP, mountsRoot) {
			continue
		}
		if _, ok := validMountPoints[cleanMP]; ok {
			continue
		}
		if err := m.unmountFn(cleanMP); err != nil && !isNotMountedError(err) {
			logrus.Warnf("failed to cleanup orphan managed mount %s: %v", cleanMP, err)
			continue
		}
		_ = os.RemoveAll(filepath.Dir(cleanMP))
	}
}

func (m *Manager) cleanupStaleLayerTempDirs() {
	layerRoots, err := os.ReadDir(m.layersDir)
	if err != nil {
		return
	}
	for _, layerRoot := range layerRoots {
		if !layerRoot.IsDir() {
			continue
		}
		rootPath := filepath.Join(m.layersDir, layerRoot.Name())
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "tmp-") {
				continue
			}
			_ = os.RemoveAll(filepath.Join(rootPath, entry.Name()))
		}
	}
}

func (m *Manager) cleanupStaleChainTempDirs() {
	chainRoots, err := os.ReadDir(m.chainsDir)
	if err != nil {
		return
	}
	for _, chainRoot := range chainRoots {
		if !chainRoot.IsDir() {
			continue
		}
		rootPath := filepath.Join(m.chainsDir, chainRoot.Name())
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "tmp-") {
				continue
			}
			_ = os.RemoveAll(filepath.Join(rootPath, entry.Name()))
		}
	}
}

func (m *Manager) cleanupOrphanChainDirs(validChainPaths map[string]struct{}) {
	chainRoots, err := os.ReadDir(m.chainsDir)
	if err != nil {
		return
	}
	for _, chainRoot := range chainRoots {
		if !chainRoot.IsDir() {
			continue
		}
		chainPath := filepath.Join(m.chainsDir, chainRoot.Name(), "fs")
		if _, ok := validChainPaths[filepath.Clean(chainPath)]; ok {
			continue
		}
		_ = os.RemoveAll(filepath.Join(m.chainsDir, chainRoot.Name()))
	}
}
