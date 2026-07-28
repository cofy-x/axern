package rollout

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type taskSelection struct {
	TaskIDs []string
	Limit   int
	Shard   *domain.TaskShard
}

type taskSelectionRecord struct {
	Request       taskSelection
	ResolvedCount int
	SelectedCount int
}

func selectTasks(tasks []domain.TaskInstance, selection taskSelection) ([]domain.TaskInstance, error) {
	selected := tasks
	if len(selection.TaskIDs) > 0 {
		selected = filterTasksByID(tasks, selection.TaskIDs)
		if len(selected) != len(selection.TaskIDs) {
			return nil, fmt.Errorf("selected task ids not found: %s", strings.Join(missingTaskIDs(tasks, selection.TaskIDs), ", "))
		}
	}
	if selection.Shard != nil {
		selected = shardTasks(selected, selection.Shard)
	}
	if selection.Limit > 0 && selection.Limit < len(selected) {
		selected = selected[:selection.Limit]
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("task selection produced no tasks")
	}
	return selected, nil
}

func shardTasks(tasks []domain.TaskInstance, shard *domain.TaskShard) []domain.TaskInstance {
	if shard == nil || shard.Count <= 0 {
		return tasks
	}
	selected := make([]domain.TaskInstance, 0, (len(tasks)+shard.Count-1)/shard.Count)
	for index, task := range tasks {
		if index%shard.Count == shard.Index {
			selected = append(selected, task)
		}
	}
	return selected
}

func filterTasksByID(tasks []domain.TaskInstance, selectedIDs []string) []domain.TaskInstance {
	wanted := make(map[string]struct{}, len(selectedIDs))
	for _, id := range selectedIDs {
		wanted[id] = struct{}{}
	}
	selected := make([]domain.TaskInstance, 0, len(selectedIDs))
	for _, task := range tasks {
		if _, ok := wanted[task.ID]; ok {
			selected = append(selected, task)
		}
	}
	return selected
}

func missingTaskIDs(tasks []domain.TaskInstance, selectedIDs []string) []string {
	found := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		found[task.ID] = struct{}{}
	}
	var missing []string
	for _, id := range selectedIDs {
		if _, ok := found[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

func newTaskSelection(record taskSelectionRecord) domain.TaskSelection {
	selection := domain.TaskSelection{
		Shard:             cloneTaskShard(record.Request.Shard),
		Limit:             record.Request.Limit,
		ResolvedTaskCount: record.ResolvedCount,
		SelectedTaskCount: record.SelectedCount,
	}
	if len(record.Request.TaskIDs) > 0 {
		selection.RequestedTaskIDs = append([]string(nil), record.Request.TaskIDs...)
	}
	return selection
}

func taskSelectionPtr(selection domain.TaskSelection) *domain.TaskSelection {
	if len(selection.RequestedTaskIDs) == 0 && selection.Limit == 0 && selection.Shard == nil {
		return nil
	}
	copied := selection
	copied.RequestedTaskIDs = append([]string(nil), selection.RequestedTaskIDs...)
	copied.Shard = cloneTaskShard(selection.Shard)
	return &copied
}

func newTaskShard(index int, count int) *domain.TaskShard {
	if count == 0 {
		return nil
	}
	return &domain.TaskShard{Index: index, Count: count}
}

func cloneTaskShard(shard *domain.TaskShard) *domain.TaskShard {
	if shard == nil {
		return nil
	}
	copied := *shard
	return &copied
}
