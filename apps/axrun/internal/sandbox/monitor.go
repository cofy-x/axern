package sandbox

import (
	"context"
	"sync"
	"time"
)

const (
	defaultHealthInterval     = 120 * time.Second
	defaultHealthThreshold    = 3
	defaultHealthProbeTimeout = 10 * time.Second
)

// HealthEvent describes a single health probe outcome.
type HealthEvent struct {
	Time             time.Time
	ConsecutiveFails int
	Err              error
	Fatal            bool
}

// MonitorOptions configures a health check Monitor.
type MonitorOptions struct {
	Interval     time.Duration
	Threshold    int
	ProbeTimeout time.Duration
	OnFailure    func(HealthEvent)
}

func (o MonitorOptions) intervalOrDefault() time.Duration {
	if o.Interval > 0 {
		return o.Interval
	}
	return defaultHealthInterval
}

func (o MonitorOptions) thresholdOrDefault() int {
	if o.Threshold > 0 {
		return o.Threshold
	}
	return defaultHealthThreshold
}

func (o MonitorOptions) probeTimeoutOrDefault() time.Duration {
	if o.ProbeTimeout > 0 {
		return o.ProbeTimeout
	}
	return defaultHealthProbeTimeout
}

// Monitor runs periodic health probes against a sandbox instance and
// declares the sandbox dead when consecutive failures reach a threshold
// or a fatal error is detected.
type Monitor struct {
	instance     Instance
	interval     time.Duration
	threshold    int
	probeTimeout time.Duration
	onFailure    func(HealthEvent)

	dead   chan struct{}
	cancel context.CancelFunc

	mu  sync.Mutex
	err error
}

// NewMonitor creates a Monitor for the given sandbox instance.
func NewMonitor(instance Instance, opts MonitorOptions) *Monitor {
	return &Monitor{
		instance:     instance,
		interval:     opts.intervalOrDefault(),
		threshold:    opts.thresholdOrDefault(),
		probeTimeout: opts.probeTimeoutOrDefault(),
		onFailure:    opts.OnFailure,
		dead:         make(chan struct{}),
	}
}

// Start begins periodic health probes in a background goroutine. The
// monitor stops when ctx is cancelled, Stop is called, or the sandbox
// is declared dead.
func (m *Monitor) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()
	go m.loop(ctx)
}

// Stop cancels the health check loop. It is safe to call multiple times.
func (m *Monitor) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Dead returns a channel that is closed when the sandbox is declared dead.
func (m *Monitor) Dead() <-chan struct{} {
	return m.dead
}

// Err returns the error that caused the sandbox to be declared dead, or
// nil if the sandbox is still considered alive.
func (m *Monitor) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

func (m *Monitor) loop(ctx context.Context) {
	consecutiveFails := 0
	if m.handleProbe(ctx, &consecutiveFails) {
		return
	}

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.handleProbe(ctx, &consecutiveFails) {
				return
			}
		}
	}
}

func (m *Monitor) handleProbe(ctx context.Context, consecutiveFails *int) bool {
	err := m.probe(ctx)
	if err == nil {
		*consecutiveFails = 0
		return false
	}

	*consecutiveFails = *consecutiveFails + 1
	fatal := IsSandboxDeath(err) || IsFatalSandboxError(err) || *consecutiveFails >= m.threshold
	event := HealthEvent{
		Time:             time.Now().UTC(),
		ConsecutiveFails: *consecutiveFails,
		Err:              err,
		Fatal:            fatal,
	}
	if m.onFailure != nil {
		m.onFailure(event)
	}
	if fatal {
		m.declareDead(err)
		return true
	}
	return false
}

func (m *Monitor) probe(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, m.probeTimeout)
	defer cancel()

	result, err := m.instance.Exec(probeCtx, ShellCommand("/bin/true"), ExecOptions{
		Timeout: m.probeTimeout,
	})
	if err != nil {
		return err
	}
	if result.ExitCode != 0 {
		return &probeNonzeroError{exitCode: result.ExitCode}
	}
	return nil
}

func (m *Monitor) declareDead(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return
	}
	m.err = err
	close(m.dead)
}

type probeNonzeroError struct {
	exitCode int
}

func (e *probeNonzeroError) Error() string {
	return "health probe exited with nonzero status"
}
