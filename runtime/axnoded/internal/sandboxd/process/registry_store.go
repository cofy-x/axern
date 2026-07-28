package process

import (
	"fmt"
	"strconv"
	"strings"
)

func (r *Registry) snapshotProcesses() []*managedProcess {
	r.mu.RLock()
	defer r.mu.RUnlock()
	processes := make([]*managedProcess, 0, len(r.procs))
	for _, managed := range r.procs {
		processes = append(processes, managed)
	}
	return processes
}

func (r *Registry) reserve(id string, managed *managedProcess) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	active := 0
	for _, item := range r.procs {
		if item.active() {
			active++
		}
	}
	if active >= maxActiveProcesses {
		return fmt.Errorf("sandboxd active process limit reached: %d: %w", maxActiveProcesses, ErrResourceLimit)
	}
	r.procs[id] = managed
	return nil
}

func (r *Registry) recordDone(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.procs[id]; !ok {
		return
	}
	r.done = append(r.done, id)
	for len(r.done) > maxRetainedProcesses {
		evict := r.done[0]
		r.done = r.done[1:]
		delete(r.procs, evict)
	}
}

func processIDLess(left, right string) bool {
	leftNumber, leftErr := strconv.ParseUint(strings.TrimPrefix(left, "proc-"), 10, 64)
	rightNumber, rightErr := strconv.ParseUint(strings.TrimPrefix(right, "proc-"), 10, 64)
	leftNumeric := strings.HasPrefix(left, "proc-") && leftErr == nil
	rightNumeric := strings.HasPrefix(right, "proc-") && rightErr == nil
	switch {
	case leftNumeric && rightNumeric:
		return leftNumber < rightNumber
	case leftNumeric != rightNumeric:
		return !leftNumeric
	default:
		return left < right
	}
}
