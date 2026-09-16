package schema

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agentprofile"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateRolloutPlan(problems *collector, runDir string, run domain.RolloutRun, plan domain.RolloutPlan, tasks taskIndex, episodes []domain.Episode) {
	planPath := filepath.Join(runDir, "plan.json")
	rel := displayPath(runDir, planPath)
	if plan.SchemaVersion != "" && plan.SchemaVersion != domain.LocalSchemaVersion {
		problems.add(rel, "schema_version", fmt.Sprintf("unsupported schema version %q", plan.SchemaVersion))
	}
	problems.required(rel, "run_id", plan.RunID)
	if run.ID != "" && plan.RunID != run.ID {
		problems.add(rel, "run_id", fmt.Sprintf("got %q, want %q", plan.RunID, run.ID))
	}
	digest, err := domain.DigestRolloutPlan(plan)
	if err != nil {
		problems.add(rel, "", fmt.Sprintf("encode plan for digest: %v", err))
	} else if run.PlanDigest != digest {
		problems.add(rel, "plan_digest", fmt.Sprintf("run records %q, want %q", run.PlanDigest, digest))
	}
	if plan.CreatedAt.IsZero() {
		problems.add(rel, "created_at", "is required")
	}
	validateInputSpec(problems, runDir, rel, "input", plan.Input)
	validatePlanSelection(problems, rel, plan.Selection, len(plan.TaskIDs), tasks.count())
	problems.requiredInt(rel, "concurrency", plan.Concurrency)
	problems.requiredInt(rel, "attempts_per_task", plan.AttemptsPerTask)
	validateAgentSpec(problems, rel, "agent", plan.Agent)
	validateProviderRequirement(problems, rel, plan.Agent, plan.Provider)
	validateModelSpec(problems, rel, "model", plan.Agent, plan.Model)
	validateSandboxSpec(problems, rel, "sandbox", plan.Sandbox)
	validateApprovalIsolation(problems, rel, plan.Agent, plan.Sandbox)
	validateSandboxRuntimeSourceRefs(problems, runDir, rel, plan.Sandbox.RuntimeSource)
	validatePlannedEpisodes(problems, rel, plan, tasks, episodes)
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
	if provider.Agent != agent.Name {
		problems.add(path, "provider.agent", fmt.Sprintf("got %q, want %q", provider.Agent, agent.Name))
	}
	providerType, err := agentprofile.ParseProviderType(provider.Provider)
	if err != nil {
		problems.add(path, "provider.provider", err.Error())
	} else if err := agentprofile.ValidateProvider(agentprofile.AgentType(agent.Name), providerType); err != nil {
		problems.add(path, "provider.provider", err.Error())
	}
	if _, err := agentprofile.ParseUpstream("provider endpoint", provider.Endpoint); err != nil {
		problems.add(path, "provider.endpoint", err.Error())
	}
	fingerprint := strings.TrimPrefix(provider.ConfigFingerprint, "sha256:")
	if !strings.HasPrefix(provider.ConfigFingerprint, "sha256:") || !sha256Pattern.MatchString(fingerprint) {
		problems.add(path, "provider.config_fingerprint", "must be a sha256: digest")
	}
}

func validatePlanSelection(problems *collector, path string, selection domain.TaskSelection, taskIDCount int, taskRecordCount int) {
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
}

func validatePlannedEpisodes(problems *collector, path string, plan domain.RolloutPlan, tasks taskIndex, episodes []domain.Episode) {
	if len(plan.Episodes) != len(episodes) {
		problems.add(path, "episodes", fmt.Sprintf("got %d planned episode(s), want %d", len(plan.Episodes), len(episodes)))
	}
	expectedCount := len(plan.TaskIDs) * plan.AttemptsPerTask
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
		if !stringSliceContains(plan.TaskIDs, planned.TaskID) {
			problems.add(path, field+".task_id", "is missing from plan.task_ids")
		}
		if plan.AttemptsPerTask > 0 && planned.AttemptIndex > plan.AttemptsPerTask {
			problems.add(path, field+".attempt_index", fmt.Sprintf("got %d, want <= attempts_per_task %d", planned.AttemptIndex, plan.AttemptsPerTask))
		}
		expectedID := domain.NewEpisodeID(plan.RunID, planned.TaskID, planned.AttemptIndex)
		if plan.RunID != "" && planned.ID != expectedID {
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

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
