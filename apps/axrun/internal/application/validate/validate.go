package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/schema"
)

func Run(params Params) (Result, error) {
	schemaResult, err := schema.ValidateRun(schema.Params{RunDir: params.RunDir})
	result := Result{
		RunID:    schemaResult.RunID,
		RunDir:   schemaResult.RunDir,
		Problems: schemaResult.Problems,
	}
	if err != nil {
		return result, err
	}
	result.Problems = append(result.Problems, validateAgentPolicies(result.RunDir)...)
	if !result.Valid() {
		return result, Error{Result: result}
	}
	return result, nil
}

func validateAgentPolicies(runDir string) []schema.Problem {
	registry := agentcatalog.DefaultRegistry()
	var problems []schema.Problem

	planPath := filepath.Join(runDir, "plan.json")
	var plan domain.RolloutPlan
	if !readJSON(&problems, runDir, planPath, &plan) {
		return problems
	}
	problems = append(problems, validateAgentSpec(registry, runDir, planPath, "agent", plan.Agent)...)
	return problems
}

func validateAgentSpec(registry *agent.Registry, runDir string, path string, field string, spec domain.AgentSpec) []schema.Problem {
	if err := registry.ValidateAgent("", spec); err != nil {
		return []schema.Problem{{
			Severity: schema.SeverityError,
			Path:     displayPath(runDir, path),
			Field:    field,
			Message:  err.Error(),
		}}
	}
	return nil
}

func readJSON[T any](problems *[]schema.Problem, runDir string, path string, value *T) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		*problems = append(*problems, schema.Problem{
			Severity: schema.SeverityError,
			Path:     displayPath(runDir, path),
			Message:  fmt.Sprintf("read JSON: %v", err),
		})
		return false
	}
	if err := json.Unmarshal(data, value); err != nil {
		*problems = append(*problems, schema.Problem{
			Severity: schema.SeverityError,
			Path:     displayPath(runDir, path),
			Message:  fmt.Sprintf("decode JSON: %v", err),
		})
		return false
	}
	return true
}

func displayPath(runDir string, path string) string {
	rel, err := filepath.Rel(runDir, path)
	if err != nil || rel == "." {
		return filepath.Clean(path)
	}
	return filepath.ToSlash(rel)
}
