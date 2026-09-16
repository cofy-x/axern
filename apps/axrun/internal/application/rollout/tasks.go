package rollout

import (
	"context"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

func newTaskInstances(ctx context.Context, params Params) (taskset.Resolved, error) {
	resolved, err := taskset.ResolveContext(ctx, params.TaskSetRef)
	if err != nil {
		return taskset.Resolved{}, err
	}
	return resolved, nil
}

func taskIDs(tasks []domain.TaskInstance) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}
