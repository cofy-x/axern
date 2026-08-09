package reconcilekernel

import (
	"sync"
	"time"
	"unicode/utf8"
)

const maxLastErrorBytes = 4096

const (
	ComponentRun        = "run"
	ComponentNode       = "node"
	ComponentService    = "service"
	ComponentAllocation = "allocation"
	ComponentCapability = "capability"
	ComponentTunnel     = "tunnel"
	ComponentFunction   = "function"
	ComponentRollout    = "rollout"
)

type ComponentHealth struct {
	Component           string     `json:"component"`
	Running             bool       `json:"running"`
	RunningSince        *time.Time `json:"running_since,omitempty"`
	LastStartedAt       *time.Time `json:"last_started_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastErrorAt         *time.Time `json:"last_error_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	ConsecutiveFailures int64      `json:"consecutive_failures"`
	nextRunID           uint64
	activeRuns          map[uint64]struct{}
	lastResultAt        *time.Time
}

// RunHandle identifies one in-flight reconcile so concurrent completions cannot
// drain or overwrite the state of a different run.
type RunHandle struct {
	component string
	id        uint64
}

type HealthSnapshot struct {
	Components []ComponentHealth `json:"components"`
}

func EmptyHealthSnapshot() HealthSnapshot {
	return HealthSnapshot{Components: []ComponentHealth{}}
}

func CountUnhealthyComponents(snapshot HealthSnapshot, now time.Time, stuckAfter time.Duration) (unhealthy, stuck int64) {
	for _, component := range snapshot.Components {
		runningSince := componentRunningSince(component)
		isStuck := component.Running && stuckAfter > 0 && runningSince != nil && now.Sub(*runningSince) >= stuckAfter
		if component.ConsecutiveFailures > 0 || isStuck {
			unhealthy++
		}
		if isStuck {
			stuck++
		}
	}
	return unhealthy, stuck
}

func componentRunningSince(component ComponentHealth) *time.Time {
	if component.RunningSince != nil {
		return component.RunningSince
	}
	return component.LastStartedAt
}

type HealthTracker struct {
	mu         sync.Mutex
	order      []string
	components map[string]*ComponentHealth
}

func NewHealthTracker(components ...string) *HealthTracker {
	tracker := &HealthTracker{components: make(map[string]*ComponentHealth)}
	for _, component := range components {
		tracker.Register(component)
	}
	return tracker
}

func (t *HealthTracker) Register(component string) {
	if t == nil || component == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.components[component]; ok {
		return
	}
	t.order = append(t.order, component)
	t.components[component] = &ComponentHealth{Component: component}
}

func (t *HealthTracker) RecordStart(component string, now time.Time) RunHandle {
	if t == nil || component == "" {
		return RunHandle{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.ensureLocked(component)
	current.nextRunID++
	handle := RunHandle{component: component, id: current.nextRunID}
	if current.activeRuns == nil {
		current.activeRuns = make(map[uint64]struct{})
	}
	startedAt := now.UTC()
	current.activeRuns[handle.id] = struct{}{}
	if current.RunningSince == nil || startedAt.Before(*current.RunningSince) {
		current.RunningSince = timePtr(now)
	}
	current.Running = true
	if current.LastStartedAt == nil || startedAt.After(*current.LastStartedAt) {
		current.LastStartedAt = timePtr(now)
	}
	return handle
}

func (t *HealthTracker) RecordResult(handle RunHandle, err error, now time.Time) {
	if t == nil || handle.component == "" || handle.id == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	current := t.components[handle.component]
	if current == nil || current.activeRuns == nil {
		return
	}
	if _, ok := current.activeRuns[handle.id]; !ok {
		return
	}
	delete(current.activeRuns, handle.id)
	current.Running = len(current.activeRuns) > 0
	if !current.Running {
		current.RunningSince = nil
	}
	finishedAt := now.UTC()
	if current.lastResultAt != nil && finishedAt.Before(*current.lastResultAt) {
		return
	}
	current.lastResultAt = timePtr(finishedAt)
	if err != nil {
		current.LastErrorAt = timePtr(finishedAt)
		current.LastError = boundedError(err.Error())
		current.ConsecutiveFailures++
		return
	}
	current.LastSuccessAt = timePtr(finishedAt)
	current.ConsecutiveFailures = 0
}

func boundedError(message string) string {
	if len(message) <= maxLastErrorBytes {
		return message
	}
	limit := maxLastErrorBytes
	for limit > 0 && !utf8.ValidString(message[:limit]) {
		limit--
	}
	return message[:limit] + "..."
}

func (t *HealthTracker) Snapshot() HealthSnapshot {
	if t == nil {
		return EmptyHealthSnapshot()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	out := HealthSnapshot{Components: make([]ComponentHealth, 0, len(t.order))}
	for _, component := range t.order {
		if current := t.components[component]; current != nil {
			out.Components = append(out.Components, cloneComponentHealth(*current))
		}
	}
	return out
}

func (t *HealthTracker) ensureLocked(component string) *ComponentHealth {
	if current := t.components[component]; current != nil {
		return current
	}
	t.order = append(t.order, component)
	current := &ComponentHealth{Component: component}
	t.components[component] = current
	return current
}

func cloneComponentHealth(in ComponentHealth) ComponentHealth {
	out := in
	out.activeRuns = nil
	out.lastResultAt = nil
	out.RunningSince = cloneTime(in.RunningSince)
	out.LastStartedAt = cloneTime(in.LastStartedAt)
	out.LastSuccessAt = cloneTime(in.LastSuccessAt)
	out.LastErrorAt = cloneTime(in.LastErrorAt)
	return out
}

func cloneTime(in *time.Time) *time.Time {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func timePtr(t time.Time) *time.Time {
	out := t.UTC()
	return &out
}
