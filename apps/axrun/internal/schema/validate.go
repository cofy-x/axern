package schema

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type Params struct {
	RunDir string
}

func ValidateRun(params Params) (Result, error) {
	rawRunDir := strings.TrimSpace(params.RunDir)
	result := Result{RunDir: filepath.Clean(rawRunDir)}
	var problems collector
	if rawRunDir == "" {
		problems.add("", "run_dir", "is required")
		result.Problems = problems.problems
		return result, ValidationError{Result: result}
	}
	runDir := filepath.Clean(rawRunDir)
	result.RunDir = runDir
	runPath := filepath.Join(runDir, "run.json")
	run, ok := readJSON[domain.RolloutRun](&problems, runDir, runPath)
	if !ok {
		result.Problems = problems.problems
		return result, ValidationError{Result: result}
	}
	result.RunID = run.ID
	validateRunRecord(&problems, runDir, runPath, run)
	planPath := filepath.Join(runDir, "plan.json")
	plan, planOK := readJSON[domain.RolloutPlan](&problems, runDir, planPath)
	if !planOK {
		result.Problems = problems.problems
		return result, ValidationError{Result: result}
	}
	tasks := validateTasks(&problems, runDir, plan.TaskIDs)
	episodes := validateEpisodes(&problems, runDir, run)
	validateRolloutPlan(&problems, runDir, run, plan, tasks, episodes)
	validateEpisodeTaskRefs(&problems, runDir, tasks, episodes)
	validateEpisodeAttemptCoverage(&problems, runDir, plan, episodes)
	validateRunSummary(&problems, runDir, runPath, run, tasks.count(), episodes)
	result.Problems = problems.problems
	if !result.Valid() {
		return result, ValidationError{Result: result}
	}
	return result, nil
}

func validateRunRecord(problems *collector, runDir string, path string, run domain.RolloutRun) {
	rel := displayPath(runDir, path)
	if run.SchemaVersion != "" && run.SchemaVersion != domain.LocalSchemaVersion {
		problems.add(rel, "schema_version", fmt.Sprintf("unsupported schema version %q", run.SchemaVersion))
	}
	problems.required(rel, "id", run.ID)
	validateRunStatus(problems, rel, "status", run.Status)
	if run.CreatedAt.IsZero() {
		problems.add(rel, "created_at", "is required")
	}
	if run.PlanPath != "plan.json" {
		problems.add(rel, "plan_path", fmt.Sprintf("got %q, want %q", run.PlanPath, "plan.json"))
	}
	fingerprint := strings.TrimPrefix(run.PlanDigest, "sha256:")
	if !strings.HasPrefix(run.PlanDigest, "sha256:") || !sha256Pattern.MatchString(fingerprint) {
		problems.add(rel, "plan_digest", "must be a sha256: digest")
	}
}
