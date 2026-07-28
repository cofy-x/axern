package oci

import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

var zeroTime time.Time

func (m *Manager) gcLayersByTTL() error {
	m.mutex.Lock()
	ttl := m.layerTTL
	now := m.now()
	m.mutex.Unlock()

	if ttl <= 0 {
		return nil
	}

	candidates, err := m.snapshotExpiredLayerCandidates(now, ttl)
	if err != nil {
		return err
	}

	removed := 0
	for _, layer := range candidates {
		deleted, err := m.tryDeleteLayerCandidate(layer, now, ttl, gcRequireExpiry, "during TTL GC")
		if err != nil {
			return err
		}
		if deleted {
			removed++
		}
	}
	if removed > 0 {
		logrus.Infof("OCI GC (ttl) finished: ttl=%s removed=%d", ttl, removed)
	}

	return nil
}

func (m *Manager) gcChainsByTTL() error {
	m.mutex.Lock()
	ttl := m.layerTTL
	now := m.now()
	m.mutex.Unlock()

	if ttl <= 0 {
		return nil
	}

	candidates, err := m.snapshotExpiredChainCandidates(now, ttl)
	if err != nil {
		return err
	}

	removed := 0
	for _, chain := range candidates {
		deleted, err := m.tryDeleteChainCandidate(chain, now, ttl, gcRequireExpiry, "during TTL GC")
		if err != nil {
			return err
		}
		if deleted {
			removed++
		}
	}
	if removed > 0 {
		logrus.Infof("OCI chain GC (ttl) finished: ttl=%s removed=%d", ttl, removed)
	}

	return nil
}

func (m *Manager) snapshotExpiredLayerCandidates(now time.Time, ttl time.Duration) ([]*LayerRecord, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	layers, err := m.store.listLayers()
	if err != nil {
		return nil, fmt.Errorf("failed to list layers: %w", err)
	}

	candidates := make([]*LayerRecord, 0, len(layers))
	for _, layer := range layers {
		if layer.RefCount != 0 || layer.RefZeroAtUnix == 0 {
			continue
		}
		if now.Sub(time.Unix(layer.RefZeroAtUnix, 0)) < ttl {
			continue
		}
		candidates = append(candidates, layer)
	}
	return candidates, nil
}

func (m *Manager) snapshotExpiredChainCandidates(now time.Time, ttl time.Duration) ([]*ChainRecord, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	chains, err := m.store.listChains()
	if err != nil {
		return nil, fmt.Errorf("failed to list chains: %w", err)
	}

	candidates := make([]*ChainRecord, 0, len(chains))
	for _, chain := range chains {
		if chain.RefCount != 0 || chain.RefZeroAtUnix == 0 {
			continue
		}
		if now.Sub(time.Unix(chain.RefZeroAtUnix, 0)) < ttl {
			continue
		}
		candidates = append(candidates, chain)
	}
	return candidates, nil
}
