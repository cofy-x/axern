package process

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func (r *Registry) Shutdown(ctx context.Context, grace time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.startMu.Lock()
	r.closing = true
	active := r.activeProcesses()
	r.startMu.Unlock()
	if len(active) == 0 {
		return nil
	}
	term, _ := proc.SignalByName("TERM")
	for _, managed := range active {
		_ = managed.closeStdin()
		if err := managed.signal(term); err != nil {
			return err
		}
	}
	if waitManagedProcesses(ctx, active, grace) {
		return ctx.Err()
	}
	callerErr := ctx.Err()
	remaining := r.activeProcesses()
	for _, managed := range remaining {
		if err := managed.kill(); err != nil {
			return errors.Join(callerErr, err)
		}
	}
	// Registry owns the child processes even after its caller stops waiting.
	// Complete a bounded local SIGKILL/reap phase before returning the caller's
	// cancellation so an abandoned request cannot leak an Allocation process.
	if waitManagedProcesses(context.Background(), remaining, time.Second) {
		return callerErr
	}
	return errors.Join(callerErr, errors.New("sandboxd process shutdown timed out"))
}

func (r *Registry) activeProcesses() []*managedProcess {
	processes := r.snapshotProcesses()
	active := processes[:0]
	for _, managed := range processes {
		if managed.active() {
			active = append(active, managed)
		}
	}
	return active
}

func waitManagedProcesses(ctx context.Context, processes []*managedProcess, timeout time.Duration) bool {
	if len(processes) == 0 {
		return true
	}
	if timeout <= 0 {
		for _, managed := range processes {
			select {
			case <-managed.done:
			default:
				return false
			}
		}
		return true
	}
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, managed := range processes {
			select {
			case <-managed.done:
			case <-waitCtx.Done():
				return
			}
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	}
}

func (p *managedProcess) signal(signal os.Signal) error {
	p.mu.RLock()
	cmd := p.cmd
	state := p.status.State
	p.mu.RUnlock()
	if state != ProcessStateRunning || cmd == nil {
		return nil
	}
	err := p.waiter.Signal(cmd, signal)
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func (p *managedProcess) kill() error {
	return p.signal(os.Kill)
}
