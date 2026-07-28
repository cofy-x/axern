package process

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

const (
	maxCapturedOutputBytes = 1 << 20
	maxActiveProcesses     = 64
	maxRetainedProcesses   = 256
	maxStreamBacklogEvents = 256
	maxTerminalDimension   = 65535
)

type Registry struct {
	waiter *proc.Waiter

	nextID uint64
	mu     sync.RWMutex
	procs  map[string]*managedProcess
	done   []string
	env    []string
	cwd    string
}

func NewRegistry(waiter *proc.Waiter, baseEnv []string, baseCwd string) *Registry {
	return &Registry{
		waiter: waiter,
		procs:  make(map[string]*managedProcess),
		env:    proc.MergeEnv(os.Environ(), baseEnv),
		cwd:    strings.TrimSpace(baseCwd),
	}
}

func (r *Registry) Status(id string) (Status, bool) {
	managed, ok := r.process(id)
	if !ok {
		return Status{}, false
	}
	return managed.snapshot(), true
}

func (r *Registry) List() ListResponse {
	processes := r.snapshotProcesses()
	statuses := make([]Status, 0, len(processes))
	for _, managed := range processes {
		statuses = append(statuses, managed.snapshot())
	}
	sort.Slice(statuses, func(i, j int) bool {
		return processIDLess(statuses[i].ID, statuses[j].ID)
	})
	return ListResponse{Processes: statuses}
}

func (r *Registry) Signal(id string, signal os.Signal) (Status, bool, error) {
	managed, ok := r.process(id)
	if !ok {
		return Status{}, false, nil
	}
	managed.mu.RLock()
	cmd := managed.cmd
	state := managed.status.State
	managed.mu.RUnlock()
	if state != ProcessStateRunning || cmd == nil || cmd.Process == nil {
		return managed.snapshot(), true, nil
	}
	if err := proc.SignalProcessGroup(cmd.Process.Pid, signal); err != nil {
		return Status{}, true, err
	}
	return managed.snapshot(), true, nil
}

func (r *Registry) WriteStdin(id string, data []byte) (Status, bool, error) {
	managed, ok := r.process(id)
	if !ok {
		return Status{}, false, nil
	}
	if err := managed.writeStdin(data); err != nil {
		return Status{}, true, err
	}
	return managed.snapshot(), true, nil
}

func (r *Registry) CloseStdin(id string) (Status, bool, error) {
	managed, ok := r.process(id)
	if !ok {
		return Status{}, false, nil
	}
	if err := managed.closeStdin(); err != nil {
		return Status{}, true, err
	}
	return managed.snapshot(), true, nil
}

func (r *Registry) Resize(id string, cols uint32, rows uint32) (Status, bool, error) {
	managed, ok := r.process(id)
	if !ok {
		return Status{}, false, nil
	}
	if err := managed.resize(cols, rows); err != nil {
		return Status{}, true, err
	}
	return managed.snapshot(), true, nil
}

func (r *Registry) SubscribeOutput(ctx context.Context, id string) (<-chan StreamEvent, bool, error) {
	managed, ok := r.process(id)
	if !ok {
		return nil, false, nil
	}
	managed.mu.RLock()
	outputs := managed.outputs
	managed.mu.RUnlock()
	if outputs == nil {
		return nil, true, fmt.Errorf("process %s was not started with streaming output", id)
	}
	return outputs.subscribe(ctx), true, nil
}

func (r *Registry) Wait(ctx context.Context, id string) (Status, bool, error) {
	managed, ok := r.process(id)
	if !ok {
		return Status{}, false, nil
	}
	select {
	case <-managed.done:
		return managed.snapshot(), true, nil
	case <-ctx.Done():
		return Status{}, true, ctx.Err()
	}
}

func (r *Registry) process(id string) (*managedProcess, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	proc, ok := r.procs[id]
	return proc, ok
}
