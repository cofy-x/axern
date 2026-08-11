package container

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
)

type containerMonitor struct {
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	mu     sync.Mutex
	err    error
}

func (m *containerMonitor) finish(err error) {
	if m != nil {
		m.once.Do(func() {
			m.mu.Lock()
			m.err = err
			m.mu.Unlock()
			close(m.done)
		})
	}
}

func (m *containerMonitor) result() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

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
			if err := m.StartMonitor(item.Val.Metadata); err != nil {
				logrus.WithError(err).WithField("container_id", item.Key).Error("start recovered container monitor")
			}
		}
	}
	if count := m.monitors.Count(); count > 0 {
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
			for item := range m.monitors.IterBuffered() {
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
	case EventTypeDelete:
		m.stopMonitor(event.ContainerID)
	case EventTypeExit:
		event = m.classifyExit(event)
		if err := m.SetExit(event.ContainerID, event.ExitCode, event.ExitCodeKnown, event.ExitedAt.String(), event.Reason, event.DiagnosticCode); err != nil {
			logrus.Errorf("set container %s exit failed: %v", event.ContainerID, err)
			return
		}
		if m.exitObserver != nil {
			m.exitObserver(event)
		}
		// Resource ownership remains assigned until the ordered Delete workflow
		// has cleaned runtime, rootfs/storage, volumes, and image leases. Exit is
		// only a lifecycle observation and must not release node capacity.
	}
}

// StartMonitor synchronously registers the runtime-exit observer before a
// successful create is exposed to callers. Registration is the lifecycle
// barrier; only the runtime Wait itself runs asynchronously.
func (m *Manager) StartMonitor(metaData *apipb.ContainerMetadata) error {
	if metaData == nil || metaData.GetID() == "" || metaData.GetRuntimeHandler() == "" {
		return errors.New("container monitor requires complete metadata identity")
	}
	handler, ok := m.serviceHandler.Get(metaData.RuntimeHandler)
	if !ok {
		return fmt.Errorf("runtime handler %s for container %s not found", metaData.RuntimeHandler, metaData.ID)
	}
	container, ok := m.containers.Get(metaData.ID)
	if !ok || container == nil || container.Status == nil {
		return fmt.Errorf("container %s monitor requires a durable status record", metaData.ID)
	}
	if container.Metadata == nil || container.Metadata.GetID() != metaData.ID || container.Metadata.GetRuntimeHandler() != metaData.RuntimeHandler {
		return fmt.Errorf("container %s monitor metadata does not match its durable runtime ownership", metaData.ID)
	}
	if container.Status.Get().State() == apipb.ContainerState_CONTAINER_EXITED {
		m.stopMonitor(metaData.ID)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	monitor := &containerMonitor{cancel: cancel, done: make(chan struct{})}
	m.monitorMu.Lock()
	if existing, ok := m.monitors.Get(metaData.ID); ok && existing != nil {
		select {
		case <-existing.done:
			m.monitors.Remove(metaData.ID)
			if container.Status.Get().State() == apipb.ContainerState_CONTAINER_EXITED {
				m.monitorMu.Unlock()
				cancel()
				return nil
			}
		default:
			m.monitorMu.Unlock()
			cancel()
			return nil
		}
	}
	absent := m.monitors.SetIfAbsent(metaData.ID, monitor)
	m.monitorMu.Unlock()
	if !absent {
		cancel()
		return nil
	}

	go m.monitorContainer(ctx, metaData, monitor, handler)
	return nil
}

func (m *Manager) monitorContainer(ctx context.Context, metaData *apipb.ContainerMetadata, monitor *containerMonitor, handler contract.RuntimeHandler) {
	logrus.Infof("start monitor container %s", metaData.ID)
	defer logrus.Infof("stop monitor container %s", metaData.ID)

	for {
		exit, err := handler.Wait(ctx, contract.HandlerOptions{ContainerID: metaData.ID})
		if ctx.Err() != nil {
			monitor.finish(ctx.Err())
			return
		}

		logrus.Infof("wait container %s finished, err: %v, exit: %+v", metaData.ID, err, exit)

		if err != nil {
			if contract.IsExitStatusUnavailable(err) {
				event := Event{
					Type:          EventTypeExit,
					ContainerID:   metaData.ID,
					Pid:           -1,
					ExitCode:      -1,
					ExitCodeKnown: false,
					ExitedAt:      time.Now(),
					Reason:        err.Error(),
				}
				classified, persistErr := m.persistMonitorExitWithRetry(ctx, event)
				if persistErr != nil {
					monitor.finish(persistErr)
					return
				}
				monitor.finish(nil)
				m.notifyMonitorExit(classified)
				return
			}
			logrus.Warnf("wait container %s failed without exit status: %v", metaData.ID, err)
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				monitor.finish(ctx.Err())
				return
			case <-timer.C:
				continue
			}
		}
		event := Event{
			Type:          EventTypeExit,
			ContainerID:   metaData.ID,
			Pid:           -1,
			ExitCode:      int32(exit.Status),
			ExitCodeKnown: true,
			ExitedAt:      exit.Timestamp,
		}
		classified, persistErr := m.persistMonitorExitWithRetry(ctx, event)
		if persistErr != nil {
			monitor.finish(persistErr)
			return
		}
		monitor.finish(nil)
		m.notifyMonitorExit(classified)
		return
	}
}

// persistMonitorExitWithRetry keeps the observed runtime exit in the monitor
// worker until the status checkpoint is durable. A transient storage failure
// must retain cleanup ownership, but it must not permanently poison every
// later Delete attempt after storage recovers.
func (m *Manager) persistMonitorExitWithRetry(ctx context.Context, event Event) (Event, error) {
	for {
		classified, err := m.persistMonitorExit(event)
		if err == nil {
			return classified, nil
		}
		logrus.WithError(err).WithField("container_id", event.ContainerID).Error("retry durable container exit checkpoint")
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return classified, errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}

// persistMonitorExit is the lifecycle barrier consumed by ordered Delete.
// Status storage is updated synchronously before monitor.done is closed; the
// observer may enqueue control-plane reporting but does not own local exit
// durability.
func (m *Manager) persistMonitorExit(event Event) (Event, error) {
	event = m.classifyExit(event)
	if err := m.SetExit(event.ContainerID, event.ExitCode, event.ExitCodeKnown, event.ExitedAt.String(), event.Reason, event.DiagnosticCode); err != nil {
		return event, fmt.Errorf("persist container %s exit: %w", event.ContainerID, err)
	}
	return event, nil
}

func (m *Manager) notifyMonitorExit(event Event) {
	if m.exitObserver != nil {
		m.exitObserver(event)
	}
}

func (m *Manager) stopMonitor(id string) {
	m.monitorMu.Lock()
	defer m.monitorMu.Unlock()
	if monitor, exists := m.monitors.Pop(id); exists && monitor != nil {
		monitor.cancel()
	}
}

func (m *Manager) waitMonitorExitBarrier(id string, timeout time.Duration) error {
	m.monitorMu.Lock()
	monitor, exists := m.monitors.Get(id)
	m.monitorMu.Unlock()
	if !exists || monitor == nil {
		container, ok := m.containers.Get(id)
		if ok && container != nil && container.Status != nil && container.Status.Get().State() == apipb.ContainerState_CONTAINER_EXITED {
			return nil
		}
		return fmt.Errorf("container %s has neither an active monitor nor a durable terminal exit checkpoint", id)
	}
	if timeout <= 0 {
		return fmt.Errorf("container %s monitor exit barrier requires a positive timeout", id)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-monitor.done:
		m.monitorMu.Lock()
		if current, ok := m.monitors.Get(id); ok && current == monitor {
			m.monitors.Remove(id)
		}
		m.monitorMu.Unlock()
		if err := monitor.result(); err != nil {
			return fmt.Errorf("container %s monitor exit-state barrier failed: %w", id, err)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("container %s monitor exit-state barrier timed out after %s", id, timeout)
	}
}

func (m *Manager) ReceiveEvent(event Event) {
	if m == nil || m.syncEventChan == nil {
		logrus.Errorf("container manager event channel is unavailable: %+v", event)
		return
	}
	if m.stopChan == nil {
		m.syncEventChan <- event
		logrus.Debugf("receive event: %+v", event)
		return
	}
	select {
	case m.syncEventChan <- event:
		logrus.Debugf("receive event: %+v", event)
	case <-m.stopChan:
		logrus.Warnf("container manager stopped before event could be received: %+v", event)
	}
}

func (m *Manager) classifyExit(event Event) Event {
	if m == nil || m.exitClassifier == nil {
		return event
	}
	diagnosticCode, reason := m.exitClassifier(event)
	event.DiagnosticCode = diagnosticCode
	if reason != "" {
		event.Reason = reason
	}
	return event
}

func (m *Manager) SetExit(id string, exitCode int32, exitCodeKnown bool, finishAt string, message string, diagnosticCode commonv1.WorkloadDiagnosticCode) error {
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
		status.DiagnosticCode = diagnosticCode
		if finishAt == "" || finishAt == "0" {
			finishAt = time.Now().Format(time.RFC3339Nano)
		}
		status.FinishedAt = finishAt
		return status, nil
	}); err != nil {
		return fmt.Errorf("checkpoint container %s exit status: %w", id, err)
	}
	return nil
}
