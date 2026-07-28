package rollout

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func normalizeParams(params Params) (Params, error) {
	params.Agent = strings.TrimSpace(params.Agent)
	params.AgentImage = strings.TrimSpace(params.AgentImage)
	params.AgentProfile = strings.TrimSpace(params.AgentProfile)
	params.AgentApprovalPolicy = strings.TrimSpace(params.AgentApprovalPolicy)
	params.AgentCommand = strings.TrimSpace(params.AgentCommand)
	params.AgentCWD = strings.TrimSpace(params.AgentCWD)
	params.AgentUser = strings.TrimSpace(params.AgentUser)
	params.AgentOutputFormat = strings.TrimSpace(params.AgentOutputFormat)
	params.AgentAllowedTools = cleanStringSlice(params.AgentAllowedTools)
	params.AgentPatchPath = strings.TrimSpace(params.AgentPatchPath)
	params.Model = strings.TrimSpace(params.Model)
	params.RuntimeClass = strings.TrimSpace(params.RuntimeClass)
	params.RunID = strings.TrimSpace(params.RunID)
	params.TaskSetRef = strings.TrimSpace(params.TaskSetRef)
	params.SelectedTaskIDs = cleanStringSlice(params.SelectedTaskIDs)
	params.ResumeRunDir = strings.TrimSpace(params.ResumeRunDir)
	params.BackendName = strings.TrimSpace(params.BackendName)
	params.Output = strings.TrimSpace(params.Output)
	if params.Attempts == 0 {
		params.Attempts = 1
	}
	if params.ResumeRunDir != "" {
		return normalizeResumeParams(params)
	}
	if params.BackendName == "" {
		params.BackendName = string(backend.NameLocal)
	}
	if params.Agent == "" {
		return Params{}, fmt.Errorf("agent is required")
	}
	backendForValidation := params.BackendName
	runtimeType := agentRuntimeType(params)
	registry := agentcatalog.DefaultRegistry()
	if err := validateAgentSelection(registry, agent.Selection{
		Name:        params.Agent,
		RuntimeType: runtimeType,
		Image:       agentImage(params),
		Profile:     params.AgentProfile,
		BackendName: backendForValidation,
	}); err != nil {
		return Params{}, err
	}
	registration, ok := registry.Lookup(params.Agent)
	if !ok {
		return Params{}, fmt.Errorf("unknown agent %q", params.Agent)
	}
	if registration.IsCommand() {
		if params.AgentCommand == "" {
			return Params{}, fmt.Errorf("command agent requires agent command")
		}
		if params.AgentProfile != "" || params.AgentApprovalPolicy != "" || params.Model != "" || params.AgentImage != "" {
			return Params{}, fmt.Errorf("command agent does not accept profile, approval policy, model, or agent image")
		}
	} else if registration.IsManaged() {
		if params.AgentCommand != "" {
			return Params{}, fmt.Errorf("managed agent %q does not accept command override", params.Agent)
		}
		if params.AgentProfile == "" {
			return Params{}, fmt.Errorf("managed agent %q requires profile", params.Agent)
		}
		if params.Model == "" {
			return Params{}, fmt.Errorf("managed agent %q requires model", params.Agent)
		}
		if err := validateApprovalPolicy(params.AgentApprovalPolicy, params.BackendName); err != nil {
			return Params{}, err
		}
	} else if params.Model == "" {
		return Params{}, fmt.Errorf("model is required")
	}
	if params.AgentTimeoutSec < 0 {
		return Params{}, fmt.Errorf("agent timeout must be positive")
	}
	if params.AgentMaxTurns < 0 {
		return Params{}, fmt.Errorf("agent max turns must be non-negative")
	}
	if params.AgentIdleTimeoutSec < 0 {
		return Params{}, fmt.Errorf("agent idle timeout must be non-negative")
	}
	if _, err := parseEnvFlags(params.AgentEnv); err != nil {
		return Params{}, err
	}
	if err := backend.ValidateName(params.BackendName); err != nil {
		return Params{}, err
	}
	if params.RunID != "" {
		if err := contract.ValidatePathSegment("rollout run id", params.RunID); err != nil {
			return Params{}, err
		}
	}
	if params.TaskSetRef == "" {
		return Params{}, fmt.Errorf("task set reference is required")
	}
	if err := validateTaskSelection(params); err != nil {
		return Params{}, err
	}
	if params.Concurrency < 1 {
		return Params{}, fmt.Errorf("concurrency must be at least 1")
	}
	if params.Attempts < 1 {
		return Params{}, fmt.Errorf("attempts must be at least 1")
	}
	if params.Output == "" {
		return Params{}, fmt.Errorf("output is required")
	}
	return params, nil
}

// NormalizeParams applies rollout parameter defaults and validates the full
// rollout request contract. It is shared by CLI and HTTP adapters so external
// entrypoints can fail fast before execution starts.
func NormalizeParams(params Params) (Params, error) {
	return normalizeParams(params)
}

func normalizeResumeParams(params Params) (Params, error) {
	if !params.Execute {
		return Params{}, fmt.Errorf("resume requires execute")
	}
	if params.Concurrency < 1 {
		return Params{}, fmt.Errorf("concurrency must be at least 1")
	}
	if params.BackendName != "" {
		return Params{}, fmt.Errorf("resume uses the runner recorded in the immutable rollout plan")
	}
	if hasCreateOnlyParams(params) {
		return Params{}, fmt.Errorf("resume cannot be combined with task, agent, model, selection, shard, or attempt creation flags")
	}
	return params, nil
}

func hasCreateOnlyParams(params Params) bool {
	return params.Agent != "" ||
		params.AgentImage != "" ||
		params.AgentProfile != "" ||
		params.AgentApprovalPolicy != "" ||
		params.AgentCommand != "" ||
		params.AgentCWD != "" ||
		params.AgentUser != "" ||
		params.AgentTimeoutSec != 0 ||
		params.AgentMaxTurns != 0 ||
		params.AgentOutputFormat != "" ||
		len(params.AgentAllowedTools) != 0 ||
		params.AgentIdleTimeoutSec != 0 ||
		params.AgentPatchPath != "" ||
		params.AgentPatchRequired ||
		len(params.AgentEnv) != 0 ||
		params.Model != "" ||
		params.RuntimeClass != "" ||
		params.RunID != "" ||
		params.TaskSetRef != "" ||
		len(params.SelectedTaskIDs) != 0 ||
		params.TaskLimit != 0 ||
		params.ShardIndex != 0 ||
		params.ShardCount != 0 ||
		params.Attempts != 1
}

func validateApprovalPolicy(value string, backendName string) error {
	policy := domain.AgentApprovalPolicy(value)
	switch policy {
	case domain.AgentApprovalPolicyNever, domain.AgentApprovalPolicyOnRequest:
	default:
		return fmt.Errorf("managed agent approval policy must be never or on_request")
	}
	switch backend.Name(backendName) {
	case backend.NameAxern:
		if policy != domain.AgentApprovalPolicyNever {
			return fmt.Errorf("Axern runner requires managed agent approval policy never")
		}
	case backend.NameLocal:
		if policy == domain.AgentApprovalPolicyNever {
			return fmt.Errorf("local runner does not allow managed agent approval policy never")
		}
	}
	return nil
}

func validateTaskSelection(params Params) error {
	if params.TaskLimit < 0 {
		return fmt.Errorf("task limit must be non-negative")
	}
	if params.ShardCount < 0 {
		return fmt.Errorf("shard count must be non-negative")
	}
	if params.ShardIndex < 0 {
		return fmt.Errorf("shard index must be non-negative")
	}
	if params.ShardIndex > 0 && params.ShardCount == 0 {
		return fmt.Errorf("shard count is required when shard index is set")
	}
	if params.ShardCount > 0 && params.ShardIndex >= params.ShardCount {
		return fmt.Errorf("shard index must be less than shard count")
	}
	seen := map[string]struct{}{}
	for _, taskID := range params.SelectedTaskIDs {
		if err := contract.ValidatePathSegment("selected task id", taskID); err != nil {
			return err
		}
		if _, exists := seen[taskID]; exists {
			return fmt.Errorf("selected task id %q is duplicated", taskID)
		}
		seen[taskID] = struct{}{}
	}
	return nil
}

func parseEnvFlags(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	env := map[string]string{}
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("agent env entries must use KEY=VALUE")
		}
		env[key] = val
	}
	if len(env) == 0 {
		return nil, nil
	}
	return env, nil
}

func cleanStringSlice(values []string) []string {
	var cleaned []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}
