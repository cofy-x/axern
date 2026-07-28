package rollout

import (
	"fmt"
	"sync"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type trajectoryRecorder struct {
	store     Store
	path      string
	now       func() time.Time
	stepIndex int
	mu        sync.Mutex
}

func newTrajectoryRecorder(store Store, path string, now func() time.Time) (*trajectoryRecorder, error) {
	stepIndex, err := store.CountTrajectorySteps(path)
	if err != nil {
		return nil, err
	}
	return &trajectoryRecorder{store: store, path: path, now: now, stepIndex: stepIndex}, nil
}

func (r *trajectoryRecorder) append(step domain.TrajectoryStep) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.stepIndex++
	step.Index = r.stepIndex
	if step.EventID == "" {
		step.EventID = fmt.Sprintf("step-%06d", r.stepIndex)
	}
	if step.Timestamp.IsZero() {
		step.Timestamp = now(r.now)
	}
	if err := r.store.AppendTrajectoryStep(r.path, step); err != nil {
		return 0, err
	}
	return r.stepIndex, nil
}

func (r *trajectoryRecorder) appendSummary(stepType domain.TrajectoryEventType, actor string, summary string) (int, error) {
	return r.append(domain.TrajectoryStep{
		Type:    stepType,
		Actor:   actor,
		Summary: summary,
	})
}
