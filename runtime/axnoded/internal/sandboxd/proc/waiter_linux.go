//go:build linux

package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const reapFallbackInterval = 100 * time.Millisecond

type child struct {
	cmd  *exec.Cmd
	done chan Result
}

type Waiter struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	waits  map[int]child
	done   chan struct{}
}

func NewWaiter(ctx context.Context) *Waiter {
	ctx, cancel := context.WithCancel(ctx)
	w := &Waiter{
		ctx:    ctx,
		cancel: cancel,
		waits:  make(map[int]child),
		done:   make(chan struct{}),
	}
	go w.reap()
	return w
}

// Start makes creation and registration atomic with respect to reaping. The
// callback may configure/start the child, but must not wait for child I/O.
func (w *Waiter) Start(cmd *exec.Cmd, start func() error) (<-chan Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		return nil, err
	}
	if err := start(); err != nil {
		return nil, err
	}
	ch := make(chan Result, 1)
	w.waits[cmd.Process.Pid] = child{cmd: cmd, done: ch}
	return ch, nil
}

// Signal serializes group signaling with wait4 and handle release. Once reaped,
// this exact command loses authority even if the PID is reused. This only covers
// the original process group, not descendants which create another session.
func (w *Waiter) Signal(cmd *exec.Cmd, signal os.Signal) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for pid, child := range w.waits {
		if child.cmd == cmd {
			err := signalProcessGroup(pid, signal)
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
	}
	return os.ErrProcessDone
}

func (w *Waiter) Stop() {
	w.cancel()
	<-w.done
}

func (w *Waiter) reap() {
	defer close(w.done)
	sigCh := make(chan os.Signal, 64)
	signal.Notify(sigCh, syscall.SIGCHLD)
	defer signal.Stop(sigCh)

	ticker := time.NewTicker(reapFallbackInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-sigCh:
			w.ReapAvailable()
		case <-ticker.C:
			w.ReapAvailable()
		}
	}
}

func (w *Waiter) ReapAvailable() {
	for {
		w.mu.Lock()
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			w.mu.Unlock()
			return
		}
		result := Result{
			ExitCode: status.ExitStatus(),
		}
		if status.Signaled() {
			result.Signal = status.Signal()
			result.ExitCode = 128 + int(status.Signal())
		}
		child, owned := w.waits[pid]
		if owned {
			delete(w.waits, pid)
			result.Err = child.cmd.Process.Release()
		}
		w.mu.Unlock()
		if owned {
			child.done <- result
			close(child.done)
		}
	}
}
