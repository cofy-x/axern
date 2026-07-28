package rollout

import (
	"context"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

type preparedRun struct {
	RolloutRun    domain.RolloutRun
	Tasks         []domain.TaskInstance
	PlanSelection domain.TaskSelection
	TaskSet       taskset.Resolved
}

func prepareRolloutRun(ctx context.Context, params Params, now time.Time) (preparedRun, error) {
	runID := params.RunID
	if runID == "" {
		generatedRunID, err := domain.NewRolloutRunID(now)
		if err != nil {
			return preparedRun{}, err
		}
		runID = generatedRunID
	}
	rolloutRun := newRolloutRun(params, runID, now)
	resolvedTaskSet, err := newTaskInstances(ctx, params)
	if err != nil {
		return preparedRun{}, err
	}
	tasks := resolvedTaskSet.Tasks
	rolloutRun.Input.Digest = resolvedTaskSet.DescriptorDigest
	rolloutRun.Input.SourceDigest = resolvedTaskSet.Descriptor.SourceDigest
	for _, payload := range resolvedTaskSet.Descriptor.Payloads {
		rolloutRun.Input.Payloads = append(rolloutRun.Input.Payloads, domain.PayloadRef{
			Format:    payload.Format,
			Reference: payload.Reference,
			Digest:    payload.Digest,
		})
	}
	resolvedTaskCount := len(tasks)
	shard := newTaskShard(params.ShardIndex, params.ShardCount)
	tasks, err = selectTasks(tasks, taskSelection{
		TaskIDs: params.SelectedTaskIDs,
		Limit:   params.TaskLimit,
		Shard:   shard,
	})
	if err != nil {
		return preparedRun{}, err
	}
	planSelection := newTaskSelection(taskSelectionRecord{
		Request: taskSelection{
			TaskIDs: params.SelectedTaskIDs,
			Limit:   params.TaskLimit,
			Shard:   shard,
		},
		ResolvedCount: resolvedTaskCount,
		SelectedCount: len(tasks),
	})
	rolloutRun.Selection = taskSelectionPtr(planSelection)
	for index := range tasks {
		if err := contract.ValidatePathSegment("task id", tasks[index].ID); err != nil {
			return preparedRun{}, err
		}
		if params.AgentTimeoutSec > 0 {
			if tasks[index].Timeouts == nil {
				tasks[index].Timeouts = &domain.TimeoutPolicy{}
			}
			tasks[index].Timeouts.AgentSec = params.AgentTimeoutSec
		}
	}
	rolloutRun.TaskIDs = taskIDs(tasks)
	rolloutRun.Timeouts = aggregateTimeouts(tasks)
	if params.AgentTimeoutSec > 0 {
		if rolloutRun.Timeouts == nil {
			rolloutRun.Timeouts = &domain.TimeoutPolicy{}
		}
		rolloutRun.Timeouts.AgentSec = params.AgentTimeoutSec
	}
	rolloutRun.Resources = commonResources(tasks)
	return preparedRun{RolloutRun: rolloutRun, Tasks: tasks, PlanSelection: planSelection, TaskSet: resolvedTaskSet}, nil
}
