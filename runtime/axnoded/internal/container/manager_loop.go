package container

import (
	"context"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/sirupsen/logrus"
)

func (m *Manager) Start() {
	if !m.started.CompareAndSwap(false, true) {
		return
	}
	m.startRecoveredMonitors()
	m.housekeeping()
	m.loop()
}

// startRecoveredMonitors runs only after startup runtime inventory
// reconciliation has removed records that are no longer owned by a live
// runtime. Starting these monitors in NewManager would race runtime Wait with
// orphan cleanup and could turn persisted recovery input into false liveness.
func (m *Manager) startRecoveredMonitors() {
	for item := range m.containers.IterBuffered() {
		if item.Val != nil && item.Val.Metadata != nil {
			m.startMonitorGoroutine(item.Val.Metadata, make(chan struct{}))
		}
	}
	if count := m.monitorStopChan.Count(); count > 0 {
		logrus.Infof("started monitors for %d runtime-reconciled containers", count)
	}
}

func (m *Manager) Stop() {
	m.stopOnce.Do(func() {
		if m.stopChan != nil {
			close(m.stopChan)
		}
		for item := range m.resourceManagers.IterBuffered() {
			_ = item.Val.ShutDown()
		}
	})
}

func (m *Manager) loop() {
	housekeepingTicker := time.NewTicker(35 * time.Second)
	defer housekeepingTicker.Stop()

	for {
		select {
		case <-housekeepingTicker.C:
			go m.housekeeping()
		case <-m.stopChan:
			logrus.Infof("container manager start to stop")
			for item := range m.monitorStopChan.IterBuffered() {
				m.stopMonitor(item.Key)
			}
			return
		case event := <-m.syncEventChan:
			go m.syncEvent(event)
		}
	}
}

func (m *Manager) syncEvent(event Event) {
	logrus.Infof("handle container event: %+v", event)
	switch event.Type {
	case EventTypeCreate:
		m.startMonitorGoroutine(event.MetaData, make(chan struct{}))
	case EventTypeDelete:
		m.stopMonitor(event.ContainerID)
	case EventTypeExit:
		if err := m.SetExit(event.ContainerID, event.ExitCode, event.ExitCodeKnown, event.ExitedAt.String(), event.Reason); err != nil {
			logrus.Errorf("set container %s exit failed: %v", event.ContainerID, err)
			return
		}
		if m.exitObserver != nil {
			m.exitObserver(event)
		}
		if err := m.ReleaseContainerResources(event.ContainerID); err != nil {
			logrus.Errorf("release resources for exited container %s failed: %v", event.ContainerID, err)
		}
	case EventTypeStart:
		container, ok := m.containers.Get(event.ContainerID)
		if !ok {
			logrus.Warnf("container %s is not ready to restart, try later", event.ContainerID)
			return
		}
		m.startMonitorGoroutine(container.Metadata, make(chan struct{}))
	}
}

// startMonitorGoroutine owns spawning the monitor worker.
func (m *Manager) startMonitorGoroutine(metaData *apipb.ContainerMetadata, stop chan struct{}) {
	handler, ok := m.serviceHandler.Get(metaData.RuntimeHandler)
	if !ok {
		logrus.Errorf("runtime handler %s for %s not found, skip it", metaData.RuntimeHandler, metaData.ID)
		return
	}

	absent := m.monitorStopChan.SetIfAbsent(metaData.ID, stop)
	if !absent {
		return
	}

	go m.monitorContainer(metaData, stop, handler)
}

func (m *Manager) monitorContainer(metaData *apipb.ContainerMetadata, stop chan struct{}, handler contract.RuntimeHandler) {
	logrus.Infof("start monitor container %s", metaData.ID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		exit, err := handler.Wait(ctx, contract.HandlerOptions{ContainerID: metaData.ID})
		if ctx.Err() != nil {
			return
		}

		logrus.Infof("wait container %s finished, err: %v, exit: %+v", metaData.ID, err, exit)

		if err != nil {
			if contract.IsExitStatusUnavailable(err) {
				m.ReceiveEvent(Event{
					Type:          EventTypeExit,
					ContainerID:   metaData.ID,
					Pid:           -1,
					ExitCode:      -1,
					ExitCodeKnown: false,
					ExitedAt:      time.Now(),
					Reason:        err.Error(),
				})
				return
			}
			logrus.Warnf("wait container %s failed without exit status: %v", metaData.ID, err)
			m.monitorStopChan.Remove(metaData.ID)
			m.ReceiveEvent(Event{
				Type:        EventTypeStart,
				ContainerID: metaData.ID,
			})
			return
		}
		m.ReceiveEvent(Event{
			Type:          EventTypeExit,
			ContainerID:   metaData.ID,
			Pid:           -1,
			ExitCode:      int32(exit.Status),
			ExitCodeKnown: true,
			ExitedAt:      exit.Timestamp,
		})
	}()

	<-stop
	logrus.Infof("stop monitor container %s", metaData.ID)
}

func (m *Manager) stopMonitor(id string) {
	if stopChan, exists := m.monitorStopChan.Pop(id); exists {
		close(stopChan)
	}
}

func (m *Manager) ReceiveEvent(event Event) {
	select {
	case m.syncEventChan <- event:
		logrus.Debugf("receive event: %+v", event)
	default:
		logrus.Warnf("event channel full, dropping event: %+v", event)
	}
}

func (m *Manager) SetExit(id string, exitCode int32, exitCodeKnown bool, finishAt string, message string) error {
	container, ok := m.containers.Get(id)
	if !ok {
		return errord.ErrNotFound
	}
	if container.Status == nil {
		return errord.ErrNotFound
	}

	if err := container.Status.UpdateSync(func(status Status) (Status, error) {
		status.Pid = -1
		status.ExitCode = exitCode
		status.ExitCodeKnown = exitCodeKnown
		status.Message = message
		if finishAt == "" || finishAt == "0" {
			finishAt = time.Now().Format(time.RFC3339Nano)
		}
		status.FinishedAt = finishAt
		return status, nil
	}); err != nil {
		logrus.Errorf("update container %s status failed: %v", id, err)
	}
	return nil
}
