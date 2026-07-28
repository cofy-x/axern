package sandbox

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	axernsdk "github.com/cofy-x/axern/sdk/go"
)

type mockInstance struct {
	mu      sync.Mutex
	execFn  func(context.Context, ExecCommand, ExecOptions) (ExecResult, error)
	closeFn func(context.Context) error
}

func (m *mockInstance) Exec(ctx context.Context, cmd ExecCommand, opts ExecOptions) (ExecResult, error) {
	m.mu.Lock()
	fn := m.execFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, cmd, opts)
	}
	return ExecResult{}, nil
}

func (m *mockInstance) UploadDir(context.Context, string, string, UploadDirOptions) error {
	return nil
}

func (m *mockInstance) DownloadPath(context.Context, string, string, DownloadPathOptions) error {
	return nil
}

func (m *mockInstance) State() (State, error) {
	return State{}, nil
}

func (m *mockInstance) Close(ctx context.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

func TestMonitorHealthyInstanceStaysAlive(t *testing.T) {
	instance := &mockInstance{}
	monitor := NewMonitor(instance, MonitorOptions{
		Interval:     10 * time.Millisecond,
		ProbeTimeout: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	monitor.Start(ctx)

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-monitor.Dead():
		t.Fatal("monitor declared healthy sandbox dead")
	default:
	}
	if err := monitor.Err(); err != nil {
		t.Fatalf("Err() = %v", err)
	}
}

func TestMonitorConsecutiveFailuresDeclareDead(t *testing.T) {
	probeErr := fmt.Errorf("connection reset")
	instance := &mockInstance{
		execFn: func(context.Context, ExecCommand, ExecOptions) (ExecResult, error) {
			return ExecResult{}, probeErr
		},
	}

	var events []HealthEvent
	var eventMu sync.Mutex
	monitor := NewMonitor(instance, MonitorOptions{
		Interval:     10 * time.Millisecond,
		Threshold:    3,
		ProbeTimeout: 5 * time.Millisecond,
		OnFailure: func(e HealthEvent) {
			eventMu.Lock()
			events = append(events, e)
			eventMu.Unlock()
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)

	select {
	case <-monitor.Dead():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sandbox death")
	}

	if err := monitor.Err(); err == nil {
		t.Fatal("Err() = nil after death")
	}

	eventMu.Lock()
	defer eventMu.Unlock()
	if len(events) < 3 {
		t.Fatalf("expected >= 3 failure events, got %d", len(events))
	}
	last := events[len(events)-1]
	if !last.Fatal {
		t.Fatal("last event should be fatal")
	}
	if last.ConsecutiveFails < 3 {
		t.Fatalf("expected consecutive_fails >= 3, got %d", last.ConsecutiveFails)
	}
}

func TestMonitorFatalErrorImmediateDeath(t *testing.T) {
	instance := &mockInstance{
		execFn: func(context.Context, ExecCommand, ExecOptions) (ExecResult, error) {
			return ExecResult{}, axernsdk.ErrSandboxNotStarted
		},
	}

	var fatalEvent HealthEvent
	var eventOnce sync.Once
	monitor := NewMonitor(instance, MonitorOptions{
		Interval:     10 * time.Millisecond,
		Threshold:    5,
		ProbeTimeout: 5 * time.Millisecond,
		OnFailure: func(e HealthEvent) {
			eventOnce.Do(func() { fatalEvent = e })
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)

	select {
	case <-monitor.Dead():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sandbox death")
	}

	if !fatalEvent.Fatal {
		t.Fatal("expected fatal event on first failure")
	}
	if fatalEvent.ConsecutiveFails != 1 {
		t.Fatalf("expected 1 consecutive fail, got %d", fatalEvent.ConsecutiveFails)
	}
}

func TestMonitorRecoveryResetsCounter(t *testing.T) {
	var callCount atomic.Int32
	instance := &mockInstance{
		execFn: func(context.Context, ExecCommand, ExecOptions) (ExecResult, error) {
			n := callCount.Add(1)
			if n <= 2 {
				return ExecResult{}, fmt.Errorf("transient failure")
			}
			return ExecResult{}, nil
		},
	}

	monitor := NewMonitor(instance, MonitorOptions{
		Interval:     10 * time.Millisecond,
		Threshold:    3,
		ProbeTimeout: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	monitor.Start(ctx)

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-monitor.Dead():
		t.Fatal("monitor declared sandbox dead despite recovery")
	default:
	}
}

func TestMonitorStopIsIdempotent(t *testing.T) {
	instance := &mockInstance{}
	monitor := NewMonitor(instance, MonitorOptions{
		Interval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)

	monitor.Stop()
	monitor.Stop()
	monitor.Stop()
}

func TestMonitorNonzeroExitCountsAsFailure(t *testing.T) {
	instance := &mockInstance{
		execFn: func(context.Context, ExecCommand, ExecOptions) (ExecResult, error) {
			return ExecResult{ExitCode: 1}, nil
		},
	}

	monitor := NewMonitor(instance, MonitorOptions{
		Interval:     10 * time.Millisecond,
		Threshold:    2,
		ProbeTimeout: 5 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)

	select {
	case <-monitor.Dead():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sandbox death from nonzero exits")
	}
}
