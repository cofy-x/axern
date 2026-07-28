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
	tasks := validateTasks(&problems, runDir, run)
	episodes := validateEpisodes(&problems, runDir, run)
	validateRolloutPlan(&problems, runDir, run, tasks, episodes)
	validateEpisodeTaskRefs(&problems, runDir, tasks, episodes)
	validateEpisodeAttemptCoverage(&problems, runDir, run, episodes)
	validateRunSelection(&problems, runDir, runPath, run, tasks.count())
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
	validateInputSpec(problems, runDir, rel, "input", run.Input)
	validateAgentSpec(problems, rel, "agent", run.Agent)
	validateModelSpec(problems, rel, "model", run.Agent, run.Model)
	validateSandboxSpec(problems, rel, "sandbox", run.Sandbox)
	validateApprovalIsolation(problems, rel, run.Agent, run.Sandbox)
	validateSandboxRuntimeSourceRefs(problems, runDir, rel, run.Sandbox.RuntimeSource)
	problems.requiredInt(rel, "concurrency", run.Concurrency)
	problems.requiredInt(rel, "attempts_per_task", run.AttemptsPerTask)
	validateRunOutputPath(problems, runDir, rel, "output_path", run.OutputPath)
}
