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
	if m.loopDone != nil {
		defer close(m.loopDone)
	}
	if m.stopped.Load() {
		return
	}
	m.startRecoveredMonitors()
	m.housekeeping()
	m.loop()
	// Stop closes admission to new workers under workerMu before loop exits, so
	// Wait cannot race a later Add. loopDone therefore covers every manager-owned
	// housekeeping/event worker as well as the dispatcher itself.
	m.workers.Wait()
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

// Stop cancels and joins runtime observers before shutting down resource
// managers. The caller owns the deadline and may retry with a fresh context if
// an external runtime handler does not complete cancellation promptly.
func (m *Manager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("container manager stop requires a context")
	}
	m.stopOnce.Do(func() {
		m.workerMu.Lock()
		m.stopped.Store(true)
		if m.stopChan != nil {
			close(m.stopChan)
		}
		m.workerMu.Unlock()
	})
	if err := m.stopAllMonitorsAndWait(ctx); err != nil {
		return err
	}
	if m.started.Load() && m.loopDone != nil {
		select {
		case <-m.loopDone:
		case <-ctx.Done():
			return fmt.Errorf("join container manager loop: %w", ctx.Err())
		}
	}
	m.resourceStopOnce.Do(func() {
		for item := range m.resourceManagers.IterBuffered() {
			if err := item.Val.ShutDown(); err != nil {
				m.resourceStopErr = errors.Join(m.resourceStopErr, fmt.Errorf("shutdown %s resource manager: %w", item.Key, err))
			}
		}
	})
	return m.resourceStopErr
}

func (m *Manager) loop() {
	housekeepingTicker := time.NewTicker(35 * time.Second)
	defer housekeepingTicker.Stop()

	for {
		select {
		case <-housekeepingTicker.C:
			m.startWorker(m.housekeeping)
		case <-m.stopChan:
			logrus.Infof("container manager start to stop")
			return
		case event := <-m.syncEventChan:
			m.startWorker(func() { m.syncEvent(event) })
		}
	}
}

func (m *Manager) startWorker(work func()) {
	if m == nil || work == nil {
		return
	}
	m.workerMu.Lock()
	if m.stopped.Load() {
		m.workerMu.Unlock()
		return
	}
	m.workers.Add(1)
	m.workerMu.Unlock()
	go func() {
		defer m.workers.Done()
		work()
	}()
}

func (m *Manager) syncEvent(event Event) {
	logrus.Infof("handle container event: %+v", event)
	switch event.Type {
	case EventTypeDelete:
		m.stopMonitor(event.ContainerID)
	default:
		// Runtime Wait is the only terminal lifecycle authority. Accepting an
		// asynchronous exit event here would bypass its checkpoint/outbox barrier.
		logrus.WithField("event_type", event.Type).Warn("ignore unsupported container manager event")
	}
}

// StartMonitor synchronously registers the runtime-exit observer before a
// successful create is exposed to callers. Registration is the lifecycle
// barrier; only the runtime Wait itself runs asynchronously.
func (m *Manager) StartMonitor(metaData *apipb.ContainerMetadata) error {
	if m.stopped.Load() {
		return errors.New("container manager is stopped")
	}
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
	if m.stopped.Load() {
		m.monitorMu.Unlock()
		cancel()
		return errors.New("container manager is stopped")
	}
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
				if err := m.notifyMonitorExitWithRetry(ctx, classified); err != nil {
					monitor.finish(err)
					return
				}
				monitor.finish(nil)
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
		if err := m.notifyMonitorExitWithRetry(ctx, classified); err != nil {
			monitor.finish(err)
			return
		}
		monitor.finish(nil)
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

// persistMonitorExit is the first lifecycle barrier consumed by ordered Delete.
// Status storage is updated synchronously, then notifyMonitorExitWithRetry
// persists the control-plane outbox before monitor.done is closed.
func (m *Manager) persistMonitorExit(event Event) (Event, error) {
	event = m.classifyExit(event)
	if event.ExitedAt.IsZero() {
		event.ExitedAt = time.Now().UTC()
	} else {
		event.ExitedAt = event.ExitedAt.UTC()
	}
	if err := m.SetExit(event.ContainerID, event.ExitCode, event.ExitCodeKnown, event.ExitedAt, event.Reason, event.DiagnosticCode); err != nil {
		return event, fmt.Errorf("persist container %s exit: %w", event.ContainerID, err)
	}
	return event, nil
}

func (m *Manager) notifyMonitorExitWithRetry(ctx context.Context, event Event) error {
	if m.exitObserver == nil {
		return nil
	}
	for {
		if err := m.exitObserver(event); err != nil {
			logrus.WithError(err).WithField("container_id", event.ContainerID).Error("retry durable container exit publication")
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return errors.Join(err, ctx.Err())
			case <-timer.C:
				continue
			}
		}
		return nil
	}
}

func (m *Manager) stopMonitor(id string) {
	m.monitorMu.Lock()
	defer m.monitorMu.Unlock()
	if monitor, exists := m.monitors.Get(id); exists && monitor != nil {
		monitor.cancel()
		select {
		case <-monitor.done:
			m.monitors.Remove(id)
		default:
			// Keep cancellation-in-progress monitors registered. Manager.Stop
			// must still join their checkpoint/report callbacks before resource
			// managers and node state can be closed.
		}
	}
}

// stopAllMonitorsAndWait is the shutdown join boundary for runtime Wait
// observers. State storage and resource managers must remain available until
// every observer has stopped persisting terminal evidence and invoking its
// exit callback.
func (m *Manager) stopAllMonitorsAndWait(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.monitorMu.Lock()
	monitors := make([]*containerMonitor, 0, m.monitors.Count())
	for item := range m.monitors.IterBuffered() {
		if item.Val == nil {
			m.monitors.Remove(item.Key)
			continue
		}
		item.Val.cancel()
		monitors = append(monitors, item.Val)
	}
	m.monitorMu.Unlock()
	for _, monitor := range monitors {
		select {
		case <-monitor.done:
		case <-ctx.Done():
			return fmt.Errorf("join container runtime monitors: %w", ctx.Err())
		}
	}
	m.monitorMu.Lock()
	for item := range m.monitors.IterBuffered() {
		select {
		case <-item.Val.done:
			m.monitors.Remove(item.Key)
		default:
		}
	}
	m.monitorMu.Unlock()
	return nil
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

func (m *Manager) SetExit(id string, exitCode int32, exitCodeKnown bool, finishedAt time.Time, message string, diagnosticCode commonv1.WorkloadDiagnosticCode) error {
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
		if finishedAt.IsZero() {
			finishedAt = time.Now().UTC()
		}
		status.FinishedAt = finishedAt.UTC().Format(time.RFC3339Nano)
		return status, nil
	}); err != nil {
		return fmt.Errorf("checkpoint container %s exit status: %w", id, err)
	}
	return nil
}
