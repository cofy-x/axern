package oci

import (
	"fmt"
	"sort"

	"github.com/sirupsen/logrus"
)

func (m *Manager) gcLayersByDiskPressure() error {
	m.mutex.Lock()
	root := m.root
	diskUsage := m.diskUsage
	m.mutex.Unlock()

	usage, err := diskUsage(root)
	if err != nil {
		return fmt.Errorf("failed to read disk usage: %w", err)
	}
	if usage < diskUsageGCStart {
		return nil
	}
	startUsage := usage

	candidates, err := m.snapshotUnusedLayerCandidates()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		logrus.Infof("OCI GC (disk pressure): usage=%.4f >= %.2f but no unreferenced layers", usage, diskUsageGCStart)
		return nil
	}
	logrus.Infof("OCI GC (disk pressure) started: usage=%.4f target<=%.2f candidates=%d", usage, diskUsageGCStop, len(candidates))

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].LastUsedUnix < candidates[j].LastUsedUnix
	})

	removed := 0
	for _, layer := range candidates {
		deleted, err := m.tryDeleteLayerCandidate(layer, zeroTime, 0, gcIgnoreAge, "during GC")
		if err != nil {
			return err
		}
		if deleted {
			removed++
		}

		usage, err = diskUsage(root)
		if err != nil {
			return fmt.Errorf("failed to refresh disk usage: %w", err)
		}
		if usage <= diskUsageGCStop {
			break
		}
	}
	logrus.Infof("OCI GC (disk pressure) finished: start_usage=%.4f end_usage=%.4f removed=%d", startUsage, usage, removed)

	return nil
}

func (m *Manager) gcChainsByDiskPressure() error {
	m.mutex.Lock()
	root := m.root
	diskUsage := m.diskUsage
	m.mutex.Unlock()

	usage, err := diskUsage(root)
	if err != nil {
		return fmt.Errorf("failed to read disk usage: %w", err)
	}
	if usage < diskUsageGCStart {
		return nil
	}
	startUsage := usage

	candidates, err := m.snapshotUnusedChainCandidates()
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		logrus.Infof("OCI chain GC (disk pressure): usage=%.4f >= %.2f but no unreferenced lowerdirs", usage, diskUsageGCStart)
		return nil
	}
	logrus.Infof("OCI chain GC (disk pressure) started: usage=%.4f target<=%.2f candidates=%d", usage, diskUsageGCStop, len(candidates))

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].LastUsedUnix < candidates[j].LastUsedUnix
	})

	removed := 0
	for _, chain := range candidates {
		deleted, err := m.tryDeleteChainCandidate(chain, zeroTime, 0, gcIgnoreAge, "during GC")
		if err != nil {
			return err
		}
		if deleted {
			removed++
		}

		usage, err = diskUsage(root)
		if err != nil {
			return fmt.Errorf("failed to refresh disk usage: %w", err)
		}
		if usage <= diskUsageGCStop {
			break
		}
	}
	logrus.Infof("OCI chain GC (disk pressure) finished: start_usage=%.4f end_usage=%.4f removed=%d", startUsage, usage, removed)

	return nil
}

func (m *Manager) snapshotUnusedLayerCandidates() ([]*LayerRecord, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	layers, err := m.store.listLayers()
	if err != nil {
		return nil, fmt.Errorf("failed to list layers: %w", err)
	}

	candidates := make([]*LayerRecord, 0, len(layers))
	for _, layer := range layers {
		if layer.RefCount == 0 {
			candidates = append(candidates, layer)
		}
	}
	return candidates, nil
}

func (m *Manager) snapshotUnusedChainCandidates() ([]*ChainRecord, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	chains, err := m.store.listChains()
	if err != nil {
		return nil, fmt.Errorf("failed to list chains: %w", err)
	}

	candidates := make([]*ChainRecord, 0, len(chains))
	for _, chain := range chains {
		if chain.RefCount == 0 {
			candidates = append(candidates, chain)
		}
	}
	return candidates, nil
}
