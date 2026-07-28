//go:build linux

package proc

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const maxCachedExitStatuses = 1024
const reapFallbackInterval = 100 * time.Millisecond

type Waiter struct {
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	waits  map[int]chan Result
	cache  map[int]Result
	order  []int
}

func NewWaiter(ctx context.Context) *Waiter {
	ctx, cancel := context.WithCancel(ctx)
	w := &Waiter{
		ctx:    ctx,
		cancel: cancel,
		waits:  make(map[int]chan Result),
		cache:  make(map[int]Result),
	}
	go w.reap()
	return w
}

func (w *Waiter) Watch(cmd *exec.Cmd) <-chan Result {
	ch := make(chan Result, 1)
	if cmd == nil || cmd.Process == nil {
		ch <- Result{ExitCode: RuntimeStartExitCode}
		close(ch)
		return ch
	}
	w.mu.Lock()
	if result, ok := w.cache[cmd.Process.Pid]; ok {
		delete(w.cache, cmd.Process.Pid)
		w.mu.Unlock()
		ch <- result
		close(ch)
		return ch
	}
	w.waits[cmd.Process.Pid] = ch
	w.mu.Unlock()
	w.ReapAvailable()
	return ch
}

func (w *Waiter) Stop() {
	w.cancel()
}

func (w *Waiter) reap() {
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
		var status syscall.WaitStatus
		pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
		if err != nil || pid <= 0 {
			return
		}
		result := Result{
			ExitCode: status.ExitStatus(),
		}
		if status.Signaled() {
			result.Signal = status.Signal()
			result.ExitCode = 128 + int(status.Signal())
		}
		w.mu.Lock()
		ch := w.waits[pid]
		if ch != nil {
			delete(w.waits, pid)
		} else {
			w.cacheResult(pid, result)
		}
		w.mu.Unlock()
		if ch != nil {
			ch <- result
			close(ch)
		}
	}
}

func (w *Waiter) cacheResult(pid int, result Result) {
	if _, exists := w.cache[pid]; !exists {
		w.order = append(w.order, pid)
	}
	w.cache[pid] = result
	for len(w.order) > maxCachedExitStatuses {
		evict := w.order[0]
		w.order = w.order[1:]
		delete(w.cache, evict)
	}
}
