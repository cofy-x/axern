package schema

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateRunSelection(problems *collector, runDir string, path string, run domain.RolloutRun, taskCount int) {
	if run.Selection == nil {
		return
	}
	rel := displayPath(runDir, path)
	for _, taskID := range run.Selection.RequestedTaskIDs {
		validatePathSegment(problems, rel, "selection.requested_task_ids", taskID)
	}
	if run.Selection.Limit < 0 {
		problems.add(rel, "selection.limit", "must be greater than or equal to zero")
	}
	validateShard(problems, rel, "selection.shard", run.Selection.Shard)
	if run.Selection.ResolvedTaskCount <= 0 {
		problems.add(rel, "selection.resolved_task_count", "must be greater than zero")
	}
	if run.Selection.SelectedTaskCount != len(run.TaskIDs) {
		problems.add(rel, "selection.selected_task_count", fmt.Sprintf("got %d, want %d", run.Selection.SelectedTaskCount, len(run.TaskIDs)))
	}
	if run.Selection.SelectedTaskCount != taskCount {
		problems.add(rel, "selection.selected_task_count", fmt.Sprintf("got %d, want task record count %d", run.Selection.SelectedTaskCount, taskCount))
	}
	if run.Selection.ResolvedTaskCount < run.Selection.SelectedTaskCount {
		problems.add(rel, "selection.resolved_task_count", "must be greater than or equal to selected_task_count")
	}
	if len(run.Selection.RequestedTaskIDs) == 0 && run.Selection.Limit == 0 && run.Selection.Shard == nil {
		problems.add(rel, "selection", "must include requested_task_ids, limit, or shard")
	}
}

func validateShard(problems *collector, path string, field string, shard *domain.TaskShard) {
	if shard == nil {
		return
	}
	if shard.Index < 0 {
		problems.add(path, field+".index", "must be greater than or equal to zero")
	}
	if shard.Count <= 0 {
		problems.add(path, field+".count", "must be greater than zero")
	}
	if shard.Count > 0 && shard.Index >= shard.Count {
		problems.add(path, field+".index", "must be less than shard.count")
	}
}
