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
	active := r.activeProcesses()
	if len(active) == 0 {
		return nil
	}
	term, _ := proc.SignalByName("TERM")
	for _, managed := range active {
		_ = managed.closeStdin()
		_ = managed.signal(term)
	}
	if waitManagedProcesses(ctx, active, grace) {
		return nil
	}
	remaining := r.activeProcesses()
	for _, managed := range remaining {
		_ = managed.kill()
	}
	if waitManagedProcesses(ctx, remaining, time.Second) {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("sandboxd process shutdown timed out")
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
	if state != ProcessStateRunning || cmd == nil || cmd.Process == nil {
		return nil
	}
	return proc.SignalProcessGroup(cmd.Process.Pid, signal)
}

func (p *managedProcess) kill() error {
	p.mu.RLock()
	cmd := p.cmd
	state := p.status.State
	p.mu.RUnlock()
	if state != ProcessStateRunning || cmd == nil || cmd.Process == nil {
		return nil
	}
	return proc.KillProcessGroup(cmd.Process.Pid)
}
