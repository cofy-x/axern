package rollout

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agentbundle"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	rolloutagents "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents"
	rolloutclaudecode "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents/claudecode"
	rolloutcodex "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents/codex"
	rolloutcommand "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents/command"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func newRolloutRun(params Params, runID string, now time.Time) domain.RolloutRun {
	return domain.RolloutRun{
		SchemaVersion:   domain.LocalSchemaVersion,
		ID:              runID,
		Status:          domain.RunStatusCreated,
		CreatedAt:       now,
		UpdatedAt:       timePtr(now),
		Input:           inputSpec(params),
		Agent:           agentSpec(params),
		Model:           domain.ModelSpec{ID: params.Model, Provider: modelProvider(params.Model)},
		Sandbox:         sandboxSpec(params),
		Concurrency:     params.Concurrency,
		AttemptsPerTask: params.Attempts,
		Metadata: domain.KeyValue{
			"created_by": "axrun",
		},
		OutputPath: filepath.Join(params.Output, runID),
	}
}

func inputSpec(params Params) *domain.InputSpec {
	return &domain.InputSpec{Type: domain.InputTypeTaskSet, Format: domain.InputFormatTaskSet, URI: params.TaskSetRef}
}

func sandboxSpec(params Params) domain.SandboxSpec {
	return domain.SandboxSpec{Backend: domain.SandboxBackend(params.BackendName)}
}

func agentSpec(params Params) domain.AgentSpec {
	requiredCapabilities := requiredCapabilitiesForAgent(params.Agent)
	if builder, ok := agentSpecBuilders()[params.Agent]; ok {
		return finalizeAgentRuntimeMetadata(builder(agentSpecParams(params, requiredCapabilities)))
	}
	spec := domain.AgentSpec{Name: params.Agent, Profile: params.AgentProfile}
	// For agents that declare a runtime record default in the registry
	// (e.g. oracle), build a minimal runtime spec from that type. Fully
	// self-contained agents such as noop may omit a runtime record.
	if runtimeType := agentRuntimeType(params); runtimeType != "" {
		spec.Runtime = registryDefaultRuntimeSpec(runtimeType, params.AgentTimeoutSec)
		spec.Capabilities = requiredCapabilities
	}
	return finalizeAgentRuntimeMetadata(spec)
}

func finalizeAgentRuntimeMetadata(spec domain.AgentSpec) domain.AgentSpec {
	if spec.Runtime == nil || spec.Runtime.Type != domain.AgentRuntimeTypeAgentImage {
		return spec
	}
	if spec.Runtime.MountTarget == "" {
		spec.Runtime.MountTarget = agentbundle.MountTarget(spec.Name)
	}
	if spec.Runtime.BinDir == "" {
		spec.Runtime.BinDir = agentbundle.BinDir(spec.Runtime.MountTarget)
	}
	return spec
}

func agentSpecBuilders() map[string]rolloutagents.SpecBuilder {
	return map[string]rolloutagents.SpecBuilder{
		rolloutclaudecode.Name: rolloutclaudecode.BuildSpec,
		rolloutcodex.Name:      rolloutcodex.BuildSpec,
		rolloutcommand.Name:    rolloutcommand.BuildSpec,
	}
}

func agentSpecParams(params Params, requiredCapabilities []string) rolloutagents.SpecParams {
	parsedEnv, _ := parseEnvFlags(params.AgentEnv)
	return rolloutagents.SpecParams{
		Name:           params.Agent,
		Profile:        params.AgentProfile,
		ApprovalPolicy: domain.AgentApprovalPolicy(params.AgentApprovalPolicy),
		RuntimeType:    agentRuntimeType(params),
		Image:          agentImage(params),
		Command:        params.AgentCommand,
		Workdir:        params.AgentCWD,
		User:           params.AgentUser,
		TimeoutSec:     params.AgentTimeoutSec,
		MaxTurns:       params.AgentMaxTurns,
		OutputFormat:   params.AgentOutputFormat,
		AllowedTools:   params.AgentAllowedTools,
		IdleTimeoutSec: params.AgentIdleTimeoutSec,
		Env:            parsedEnv,
		PatchPath:      params.AgentPatchPath,
		PatchRequired:  params.AgentPatchRequired,
		Capabilities:   requiredCapabilities,
	}
}

// registryDefaultRuntimeSpec returns a minimal AgentRuntimeSpec for agents
// that do not require rich CLI-driven configuration. It captures stdout/stderr
// so run records are always usable for inspection even without an LLM trace.
func registryDefaultRuntimeSpec(runtimeType domain.AgentRuntimeType, timeoutSec int) *domain.AgentRuntimeSpec {
	return &domain.AgentRuntimeSpec{
		Type:       runtimeType,
		TimeoutSec: timeoutSec,
		Artifacts: &domain.ArtifactPolicySpec{
			CaptureStdout: true,
			CaptureStderr: true,
		},
	}
}

func requiredCapabilitiesForAgent(agentName string) []string {
	registry := agentcatalog.DefaultRegistry()
	registration, ok := registry.Lookup(agentName)
	if !ok || len(registration.RequiredCapabilities) == 0 {
		return nil
	}
	return append([]string(nil), registration.RequiredCapabilities...)
}

func agentRuntimeType(params Params) domain.AgentRuntimeType {
	return inferAgentRuntimeTypeForBackend(params.Agent, params.AgentImage, params.AgentCommand, params.BackendName)
}

func agentImage(params Params) string {
	return strings.TrimSpace(params.AgentImage)
}

func inferAgentRuntimeType(agentName string, agentImage string) domain.AgentRuntimeType {
	reg, ok := agentcatalog.DefaultRegistry().Lookup(agentName)
	if ok && reg.DefaultRuntimeType != "" {
		return reg.DefaultRuntimeType
	}
	if ok && reg.IsSelfContained() {
		return ""
	}
	if ok && agentImage != "" && reg.SupportsRuntime(domain.AgentRuntimeTypeAgentImage) {
		return domain.AgentRuntimeTypeAgentImage
	}
	return domain.AgentRuntimeTypeSandboxCommand
}

func inferAgentRuntimeTypeForBackend(agentName string, agentImage string, agentCommand string, backendName string) domain.AgentRuntimeType {
	if strings.TrimSpace(agentImage) != "" {
		return inferAgentRuntimeType(agentName, agentImage)
	}
	if strings.TrimSpace(agentCommand) != "" {
		return domain.AgentRuntimeTypeSandboxCommand
	}
	if backend.Name(backendName) != backend.NameAxern {
		return inferAgentRuntimeType(agentName, agentImage)
	}
	reg, ok := agentcatalog.DefaultRegistry().Lookup(agentName)
	if ok && reg.SupportsRuntime(domain.AgentRuntimeTypeAgentImage) && !reg.IsSelfContained() {
		return domain.AgentRuntimeTypeAgentImage
	}
	return inferAgentRuntimeType(agentName, agentImage)
}

func newEpisode(rolloutRun domain.RolloutRun, task domain.TaskInstance, attemptIndex int) domain.Episode {
	return domain.Episode{
		ID:           domain.NewEpisodeID(rolloutRun.ID, task.ID, attemptIndex),
		RunID:        rolloutRun.ID,
		TaskID:       task.ID,
		AttemptIndex: attemptIndex,
		Status:       domain.EpisodeStatusPending,
		Agent:        rolloutRun.Agent,
		Model:        rolloutRun.Model,
		Sandbox:      task.Sandbox,
	}
}

func modelProvider(modelID string) string {
	before, _, ok := strings.Cut(modelID, "/")
	if !ok {
		return ""
	}
	return before
}
