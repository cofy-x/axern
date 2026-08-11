package container

import (
	"context"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"os"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
)

func (m *Manager) housekeeping() {
	isRunning := m.isHousekeepingRunning.Swap(true)
	if isRunning {
		return
	}
	defer m.isHousekeepingRunning.Store(false)

	logrus.Debugf("start housekeeping: %d containers", m.containers.Count())
	start := time.Now()
	defer func() {
		spent := time.Since(start)
		if spent >= config.HouseKeepingMaxCostTime {
			logrus.Warnf("housekeeping finished, cost %v which is too long", spent)
			m.healthChan <- false
		} else {
			logrus.Debugf("housekeeping finished, cost %v", spent)
			m.healthChan <- true
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cstates := make(map[string]*contract.UnionContainerState)

	for r, handler := range m.serviceHandler.Items() {
		states, err := handler.ListContainers(ctx, contract.HandlerOptions{})
		if err != nil {
			logrus.Errorf("list %s containers failed: %v", r, err)
			continue
		}
		for idx := range states {
			cstates[states[idx].ID] = states[idx]
		}
	}

	for id, container := range m.containers.Items() {
		if container == nil {
			m.containers.Remove(id)
			continue
		}

		if _, err := os.Stat(container.PATH); err != nil && os.IsNotExist(err) {
			logrus.Errorf("container %s root %s is not exist", id, container.PATH)
			m.ReceiveEvent(Event{Type: EventTypeDelete, ContainerID: id})
			continue
		}

		if container.Status != nil {
			if err := container.Status.UpdateSync(func(status Status) (Status, error) {
				return UpdateStatusByState(cstates[id], status), nil
			}); err != nil {
				logrus.Errorf("update container %s status failed: %v", id, err)
			}
		}

		if container.Status == nil && cstates[id] != nil {
			container.Status = GenerateStatusFromState(cstates[id], filepath.Join(container.PATH, config.ContainerStatusFile))
		}

		if container.Status == nil {
			logrus.Errorf("container %s status is nil", id)
			m.containers.Remove(id)
			continue
		}

		// Exited allocations retain all resource claims until the authoritative
		// Delete workflow completes ordered cleanup.
	}

	dir, err := os.ReadDir(m.recyclePath)
	if err == nil {
		for _, d := range dir {
			os.RemoveAll(filepath.Join(m.recyclePath, d.Name()))
		}
	}

	for item := range m.containers.IterBuffered() {
		if err := m.StartMonitor(item.Val.Metadata); err != nil {
			logrus.WithError(err).WithField("container_id", item.Key).Error("start container monitor during housekeeping")
		}
	}

	metrics.RecordResourceGauge("container", float64(m.containers.Count()))

	for item := range m.monitors.IterBuffered() {
		if !m.containers.Has(item.Key) {
			logrus.Infof("container %s is deleted, release releated resource", item.Key)
			m.ReceiveEvent(Event{Type: EventTypeDelete, ContainerID: item.Key})
		}
	}
}
