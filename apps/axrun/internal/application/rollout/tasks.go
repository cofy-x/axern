package rollout

import (
	"context"
	"reflect"

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

func aggregateTimeouts(tasks []domain.TaskInstance) *domain.TimeoutPolicy {
	var result *domain.TimeoutPolicy
	for _, task := range tasks {
		if task.Timeouts == nil {
			continue
		}
		if result == nil {
			copied := *task.Timeouts
			result = &copied
			continue
		}
		if task.Timeouts.AgentSec > result.AgentSec {
			result.AgentSec = task.Timeouts.AgentSec
		}
		if task.Timeouts.VerifierSec > result.VerifierSec {
			result.VerifierSec = task.Timeouts.VerifierSec
		}
		if task.Timeouts.EpisodeSec > result.EpisodeSec {
			result.EpisodeSec = task.Timeouts.EpisodeSec
		}
	}
	return result
}

func commonResources(tasks []domain.TaskInstance) *domain.ResourceSpec {
	var result *domain.ResourceSpec
	for _, task := range tasks {
		if task.Resources == nil {
			continue
		}
		if result == nil {
			copied := *task.Resources
			result = &copied
			continue
		}
		if !reflect.DeepEqual(*result, *task.Resources) {
			return nil
		}
	}
	return result
}
