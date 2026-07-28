package schema

import (
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

func validateRolloutPlan(problems *collector, runDir string, run domain.RolloutRun, tasks taskIndex, episodes []domain.Episode) *domain.RolloutPlan {
	planPath := filepath.Join(runDir, "plan.json")
	plan, ok := readJSON[domain.RolloutPlan](problems, runDir, planPath)
	if !ok {
		return nil
	}
	rel := displayPath(runDir, planPath)
	if plan.SchemaVersion != "" && plan.SchemaVersion != domain.LocalSchemaVersion {
		problems.add(rel, "schema_version", fmt.Sprintf("unsupported schema version %q", plan.SchemaVersion))
	}
	problems.required(rel, "run_id", plan.RunID)
	if run.ID != "" && plan.RunID != run.ID {
		problems.add(rel, "run_id", fmt.Sprintf("got %q, want %q", plan.RunID, run.ID))
	}
	if plan.CreatedAt.IsZero() {
		problems.add(rel, "created_at", "is required")
	}
	validateInputSpec(problems, runDir, rel, "input", plan.Input)
	validatePlanInputMatchesRun(problems, rel, plan.Input, run.Input)
	validatePlanSelection(problems, rel, plan.Selection, run.Selection, len(run.TaskIDs), tasks.count())
	comparePlanInt(problems, rel, "concurrency", plan.Concurrency, run.Concurrency)
	comparePlanInt(problems, rel, "attempts_per_task", plan.AttemptsPerTask, run.AttemptsPerTask)
	if !reflect.DeepEqual(plan.Agent, run.Agent) {
		problems.add(rel, "agent", "must match run.agent")
	}
	validateProviderRequirement(problems, rel, plan.Agent, plan.Provider)
	if !reflect.DeepEqual(plan.Model, run.Model) {
		problems.add(rel, "model", "must match run.model")
	}
	if !reflect.DeepEqual(plan.Sandbox, run.Sandbox) {
		problems.add(rel, "sandbox", "must match run.sandbox")
	}
	if !reflect.DeepEqual(plan.TaskIDs, run.TaskIDs) {
		problems.add(rel, "task_ids", fmt.Sprintf("got %#v, want %#v", plan.TaskIDs, run.TaskIDs))
	}
	validatePlannedEpisodes(problems, rel, run, plan, tasks, episodes)
	return &plan
}

func validateProviderRequirement(problems *collector, path string, agent domain.AgentSpec, provider *domain.ProviderRequirement) {
	wireAPI, err := agentprofile.RequiredWireAPI(agentprofile.AgentType(agent.Name))
	if err != nil {
		if provider != nil {
			problems.add(path, "provider", "must be absent for an agent without a managed provider")
		}
		return
	}
	if provider == nil {
		problems.add(path, "provider", "is required for a managed agent")
		return
	}
	if provider.WireAPI != string(wireAPI) {
		problems.add(path, "provider.wire_api", fmt.Sprintf("got %q, want %q", provider.WireAPI, wireAPI))
	}
}

func validatePlanInputMatchesRun(problems *collector, path string, planInput *domain.InputSpec, runInput *domain.InputSpec) {
	if !reflect.DeepEqual(planInput, runInput) {
		problems.add(path, "input", "must match run.input")
	}
}

func validatePlanSelection(problems *collector, path string, selection domain.TaskSelection, runSelection *domain.TaskSelection, taskIDCount int, taskRecordCount int) {
	for _, taskID := range selection.RequestedTaskIDs {
		validatePathSegment(problems, path, "selection.requested_task_ids", taskID)
	}
	if selection.Limit < 0 {
		problems.add(path, "selection.limit", "must be greater than or equal to zero")
	}
	validateShard(problems, path, "selection.shard", selection.Shard)
	if selection.ResolvedTaskCount <= 0 {
		problems.add(path, "selection.resolved_task_count", "must be greater than zero")
	}
	if selection.SelectedTaskCount != taskIDCount {
		problems.add(path, "selection.selected_task_count", fmt.Sprintf("got %d, want %d", selection.SelectedTaskCount, taskIDCount))
	}
	if selection.SelectedTaskCount != taskRecordCount {
		problems.add(path, "selection.selected_task_count", fmt.Sprintf("got %d, want task record count %d", selection.SelectedTaskCount, taskRecordCount))
	}
	if selection.ResolvedTaskCount < selection.SelectedTaskCount {
		problems.add(path, "selection.resolved_task_count", "must be greater than or equal to selected_task_count")
	}
	if runSelection == nil {
		if len(selection.RequestedTaskIDs) > 0 || selection.Limit != 0 || selection.Shard != nil {
			problems.add(path, "selection", "must match absent run.selection")
		}
		return
	}
	if !reflect.DeepEqual(selection.RequestedTaskIDs, runSelection.RequestedTaskIDs) {
		problems.add(path, "selection.requested_task_ids", "must match run.selection.requested_task_ids")
	}
	if selection.Limit != runSelection.Limit {
		problems.add(path, "selection.limit", fmt.Sprintf("got %d, want %d", selection.Limit, runSelection.Limit))
	}
	if !reflect.DeepEqual(selection.Shard, runSelection.Shard) {
		problems.add(path, "selection.shard", "must match run.selection.shard")
	}
	if selection.ResolvedTaskCount != runSelection.ResolvedTaskCount {
		problems.add(path, "selection.resolved_task_count", fmt.Sprintf("got %d, want %d", selection.ResolvedTaskCount, runSelection.ResolvedTaskCount))
	}
	if selection.SelectedTaskCount != runSelection.SelectedTaskCount {
		problems.add(path, "selection.selected_task_count", fmt.Sprintf("got %d, want %d", selection.SelectedTaskCount, runSelection.SelectedTaskCount))
	}
}

func validatePlannedEpisodes(problems *collector, path string, run domain.RolloutRun, plan domain.RolloutPlan, tasks taskIndex, episodes []domain.Episode) {
	if len(plan.Episodes) != len(episodes) {
		problems.add(path, "episodes", fmt.Sprintf("got %d planned episode(s), want %d", len(plan.Episodes), len(episodes)))
	}
	expectedCount := len(run.TaskIDs) * run.AttemptsPerTask
	if expectedCount > 0 && len(plan.Episodes) != expectedCount {
		problems.add(path, "episodes", fmt.Sprintf("got %d planned episode(s), want %d from task_ids * attempts_per_task", len(plan.Episodes), expectedCount))
	}
	actual := map[string]domain.Episode{}
	for _, episode := range episodes {
		actual[episode.ID] = episode
	}
	seen := map[string]struct{}{}
	for index, planned := range plan.Episodes {
		field := fmt.Sprintf("episodes[%d]", index)
		problems.required(path, field+".id", planned.ID)
		validatePathSegment(problems, path, field+".id", planned.ID)
		problems.required(path, field+".task_id", planned.TaskID)
		validatePathSegment(problems, path, field+".task_id", planned.TaskID)
		problems.requiredInt(path, field+".attempt_index", planned.AttemptIndex)
		problems.requiredInt(path, field+".order", planned.Order)
		if planned.Order != index+1 {
			problems.add(path, field+".order", fmt.Sprintf("got %d, want %d", planned.Order, index+1))
		}
		if _, ok := tasks[planned.TaskID]; !ok {
			problems.add(path, field+".task_id", fmt.Sprintf("referenced task %q is missing", planned.TaskID))
		}
		if !stringSliceContains(run.TaskIDs, planned.TaskID) {
			problems.add(path, field+".task_id", "is missing from run.task_ids")
		}
		if run.AttemptsPerTask > 0 && planned.AttemptIndex > run.AttemptsPerTask {
			problems.add(path, field+".attempt_index", fmt.Sprintf("got %d, want <= attempts_per_task %d", planned.AttemptIndex, run.AttemptsPerTask))
		}
		expectedID := domain.NewEpisodeID(run.ID, planned.TaskID, planned.AttemptIndex)
		if run.ID != "" && planned.ID != expectedID {
			problems.add(path, field+".id", fmt.Sprintf("got %q, want %q", planned.ID, expectedID))
		}
		if _, exists := seen[planned.ID]; exists {
			problems.add(path, field+".id", "duplicates earlier planned episode")
		}
		seen[planned.ID] = struct{}{}
		episode, ok := actual[planned.ID]
		if !ok {
			problems.add(path, field+".id", "planned episode record is missing")
			continue
		}
		if episode.TaskID != planned.TaskID {
			problems.add(path, field+".task_id", fmt.Sprintf("episode record has task_id %q", episode.TaskID))
		}
		if episode.AttemptIndex != planned.AttemptIndex {
			problems.add(path, field+".attempt_index", fmt.Sprintf("episode record has attempt_index %d", episode.AttemptIndex))
		}
	}
}

func comparePlanInt(problems *collector, path string, field string, got int, want int) {
	if got != want {
		problems.add(path, field, fmt.Sprintf("got %d, want %d", got, want))
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
