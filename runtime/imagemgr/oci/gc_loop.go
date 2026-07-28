package oci

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// pruneImagesLoop runs GC checks on interval.
func (m *Manager) pruneImagesLoop() {
	ticker := time.NewTicker(PruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.gcLayers(); err != nil {
				logrus.Warnf("OCI GC check failed: %v", err)
			}
		case <-m.stopCh:
			return
		}
	}
}

func (m *Manager) gcLayers() error {
	return m.gcLayersWithContext(context.Background())
}

func (m *Manager) gcLayersWithContext(ctx context.Context) error {
	timing, _ := StartOCITimedOperation(ctx, "oci.GCLayers", m.root)
	defer timing.End()

	stageStart := time.Now()
	if err := m.gcChainsByTTL(); err != nil {
		timing.RecordError(err)
		return err
	}
	timing.Stage("chain_ttl_gc", time.Since(stageStart))

	stageStart = time.Now()
	if err := m.gcLayersByTTL(); err != nil {
		timing.RecordError(err)
		return err
	}
	timing.Stage("ttl_gc", time.Since(stageStart))

	stageStart = time.Now()
	if err := m.gcChainsByDiskPressure(); err != nil {
		timing.RecordError(err)
		return err
	}
	timing.Stage("chain_disk_pressure_gc", time.Since(stageStart))

	stageStart = time.Now()
	if err := m.gcLayersByDiskPressure(); err != nil {
		timing.RecordError(err)
		return err
	}
	timing.Stage("disk_pressure_gc", time.Since(stageStart))

	return nil
}
