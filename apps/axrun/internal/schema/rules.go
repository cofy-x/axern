package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

var sha256Pattern = regexp.MustCompile("^[a-f0-9]{64}$")

func validateAgentSpec(problems *collector, path string, field string, agent domain.AgentSpec) {
	problems.required(path, field+".name", agent.Name)
	switch agent.Name {
	case "command":
		problems.empty(path, field+".profile", agent.Profile)
		problems.empty(path, field+".approval_policy", string(agent.ApprovalPolicy))
		if agent.Runtime == nil || len(agent.Runtime.Command) == 0 {
			problems.add(path, field+".runtime.command", "is required for command agent")
		}
	case "claude-code", "codex":
		problems.required(path, field+".profile", agent.Profile)
		if agent.Runtime != nil && len(agent.Runtime.Command) > 0 {
			problems.add(path, field+".runtime.command", "must be empty for managed agent")
		}
		if agent.ApprovalPolicy != domain.AgentApprovalPolicyNever && agent.ApprovalPolicy != domain.AgentApprovalPolicyOnRequest {
			problems.add(path, field+".approval_policy", "must be never or on_request")
		}
	}
	validateAgentRuntimeSpec(problems, path, field+".runtime", agent.Runtime)
}

func validateAgentRuntimeSpec(problems *collector, path string, field string, runtime *domain.AgentRuntimeSpec) {
	if runtime == nil {
		return
	}
	for _, problem := range contract.ValidateAgentRuntimeSpec(runtime) {
		problems.add(path, field+"."+problem.Field, problem.Message)
	}
	validatePromptSpec(problems, path, field+".prompt", runtime.Prompt)
	validateAgentSessionSpec(problems, path, field+".session", runtime.Session)
}

func validatePromptSpec(problems *collector, path string, field string, prompt *domain.PromptSpec) {
	if prompt == nil {
		return
	}
	validatePromptSource(problems, path, field+".source", prompt.Source)
	for index, round := range prompt.Rounds {
		prefix := fmt.Sprintf("%s.rounds[%d]", field, index)
		if round.Index < 1 {
			problems.add(path, prefix+".index", "must be greater than or equal to one")
		}
		validatePromptSource(problems, path, prefix+".source", round.Source)
		validateRelativeRef(problems, path, prefix+".rendered_prompt_ref", round.RenderedPromptRef)
	}
}

func validatePromptSource(problems *collector, path string, field string, source domain.PromptSourceType) {
	switch source {
	case "", domain.PromptSourceInstruction, domain.PromptSourceInline, domain.PromptSourceTemplate:
		return
	default:
		problems.add(path, field, fmt.Sprintf("unsupported prompt source %q", source))
	}
}

func validateAgentSessionSpec(problems *collector, path string, field string, session *domain.AgentSessionSpec) {
	if session == nil {
		return
	}
	switch session.Mode {
	case "", domain.AgentSessionModeNone, domain.AgentSessionModeCreate, domain.AgentSessionModeResume, domain.AgentSessionModeMultiRound:
		return
	default:
		problems.add(path, field+".mode", fmt.Sprintf("unsupported agent session mode %q", session.Mode))
	}
}

func validateModelSpec(problems *collector, path string, field string, agent domain.AgentSpec, model domain.ModelSpec) {
	if agent.Name == "command" {
		if model.ID != "" || model.Provider != "" || model.EndpointFamily != "" || model.Effort != "" || model.TokenBudget != 0 {
			problems.add(path, field, "must be empty for command agent")
		}
		return
	}
	problems.required(path, field+".id", model.ID)
}

func validateSandboxSpec(problems *collector, path string, field string, sandbox domain.SandboxSpec) {
	problems.required(path, field+".backend", string(sandbox.Backend))
	validateSandboxRuntimeSource(problems, path, field+".runtime_source", sandbox.RuntimeSource)
}

func validateApprovalIsolation(problems *collector, path string, agent domain.AgentSpec, sandbox domain.SandboxSpec) {
	if agent.Name != "claude-code" && agent.Name != "codex" {
		return
	}
	switch sandbox.Backend {
	case domain.SandboxBackendAxern:
		if agent.ApprovalPolicy != domain.AgentApprovalPolicyNever {
			problems.add(path, "agent.approval_policy", "must be never for Axern sandbox isolation")
		}
	case domain.SandboxBackendLocal:
		if agent.ApprovalPolicy == domain.AgentApprovalPolicyNever {
			problems.add(path, "agent.approval_policy", "must not be never for local host execution")
		}
	}
}

func validateSandboxRuntimeSource(problems *collector, path string, field string, source *domain.SandboxRuntimeSourceSpec) {
	if source == nil {
		return
	}
	if !contract.IsSandboxRuntimeSourceType(source.Type) {
		problems.add(path, field+".type", fmt.Sprintf("unsupported runtime source type %q", source.Type))
		return
	}
	switch source.Type {
	case domain.SandboxRuntimeSourceTemplate:
		problems.required(path, field+".template_id", source.TemplateID)
		problems.empty(path, field+".image", source.Image)
		problems.empty(path, field+".dockerfile", source.Dockerfile)
	case domain.SandboxRuntimeSourceImage:
		problems.required(path, field+".image", source.Image)
		problems.empty(path, field+".template_id", source.TemplateID)
		problems.empty(path, field+".dockerfile", source.Dockerfile)
	case domain.SandboxRuntimeSourceDockerfile:
		problems.required(path, field+".dockerfile", source.Dockerfile)
		problems.empty(path, field+".template_id", source.TemplateID)
		problems.empty(path, field+".image", source.Image)
	}
}

func validateVerifierSpec(problems *collector, path string, field string, verifier domain.VerifierSpec) {
	if !contract.IsVerifierType(verifier.Type) {
		validateVerifierType(problems, path, field+".type", verifier.Type)
		return
	}
	if verifier.Type == domain.VerifierTypeShell && verifier.Command == "" {
		problems.required(path, field+".command", verifier.Command)
		return
	}
	if err := contract.ValidateVerifierSpec("task", verifier); err != nil {
		problems.add(path, field, err.Error())
	}
}

func validateVerifierType(problems *collector, path string, field string, value domain.VerifierType) {
	if !contract.IsVerifierType(value) {
		problems.add(path, field, fmt.Sprintf("unsupported verifier type %q", value))
	}
}

func validateArtifactRefs(problems *collector, runDir string, path string, field string, artifacts []domain.ArtifactRef) {
	for index, artifact := range artifacts {
		prefix := fmt.Sprintf("%s[%d]", field, index)
		validateRunRef(problems, runDir, path, prefix+".path", artifact.Path, true)
		if artifact.Kind != "" {
			validateArtifactKind(problems, path, prefix+".kind", artifact.Kind)
		}
		if artifact.Role != "" {
			validateArtifactRole(problems, path, prefix+".role", artifact.Role)
		}
		if artifact.SizeBytes < 0 {
			problems.add(path, prefix+".size_bytes", "must be greater than or equal to zero")
		}
		if artifact.SHA256 != "" && !sha256Pattern.MatchString(artifact.SHA256) {
			problems.add(path, prefix+".sha256", "must be a 64-character lowercase hex digest")
		}
		if artifact.MediaType != "" && !strings.Contains(artifact.MediaType, "/") {
			problems.add(path, prefix+".media_type", "must be a valid media type")
		}
	}
}

func validatePathSegment(problems *collector, path string, field string, value string) {
	if value == "" {
		return
	}
	if contract.ValidatePathSegment(field, value) != nil {
		problems.add(path, field, "must be a single path segment")
	}
}

func validateRunStatus(problems *collector, path string, field string, value domain.RunStatus) {
	if !contract.IsRunStatus(value) {
		problems.add(path, field, fmt.Sprintf("unsupported run status %q", value))
	}
}

func validateEpisodeStatus(problems *collector, path string, field string, value domain.EpisodeStatus) {
	if !contract.IsEpisodeStatus(value) {
		problems.add(path, field, fmt.Sprintf("unsupported episode status %q", value))
	}
}

func validateAgentStatus(problems *collector, path string, field string, value domain.AgentStatus) {
	if !contract.IsAgentStatus(value) {
		problems.add(path, field, fmt.Sprintf("unsupported agent status %q", value))
	}
}

func validateAgentExitReason(problems *collector, path string, field string, value domain.AgentExitReason) {
	if value != "" && !contract.IsAgentExitReason(value) {
		problems.add(path, field, fmt.Sprintf("unsupported agent exit reason %q", value))
	}
}

func validateFinalAgentStatus(problems *collector, path string, field string, value domain.AgentStatus) {
	if contract.IsFinalAgentStatus(value) {
		return
	}
	if contract.IsAgentStatus(value) {
		problems.add(path, field, fmt.Sprintf("agent result must be final, got %q", value))
	}
}

func validateRewardStatus(problems *collector, path string, field string, value domain.RewardStatus) {
	if !contract.IsRewardStatus(value) {
		problems.add(path, field, fmt.Sprintf("unsupported reward status %q", value))
	}
}

func validateFailureClass(problems *collector, path string, field string, value domain.FailureClass) {
	if !contract.IsFailureClass(value) {
		problems.add(path, field, fmt.Sprintf("unsupported failure class %q", value))
	}
}

func validateTrajectoryEventType(problems *collector, path string, field string, value domain.TrajectoryEventType) {
	if !contract.IsTrajectoryEventType(value) {
		problems.add(path, field, fmt.Sprintf("unsupported trajectory event type %q", value))
	}
}

func validateArtifactKind(problems *collector, path string, field string, value domain.ArtifactKind) {
	if !contract.IsArtifactKind(value) {
		problems.add(path, field, fmt.Sprintf("unsupported artifact kind %q", value))
	}
}

func validateArtifactRole(problems *collector, path string, field string, value domain.ArtifactRole) {
	if !contract.IsArtifactRole(value) {
		problems.add(path, field, fmt.Sprintf("unsupported artifact role %q", value))
	}
}

func readJSON[T any](problems *collector, runDir string, path string) (T, bool) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		problems.add(displayPath(runDir, path), "", fmt.Sprintf("read JSON: %v", err))
		return zero, false
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		problems.add(displayPath(runDir, path), "", fmt.Sprintf("decode JSON: %v", err))
		return zero, false
	}
	return value, true
}
