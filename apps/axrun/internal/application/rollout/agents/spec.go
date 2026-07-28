package agents

import (
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agentbundle"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type SpecParams struct {
	Name           string
	Profile        string
	ApprovalPolicy domain.AgentApprovalPolicy
	RuntimeType    domain.AgentRuntimeType
	Image          string
	Command        string
	Workdir        string
	User           string
	TimeoutSec     int
	MaxTurns       int
	OutputFormat   string
	AllowedTools   []string
	IdleTimeoutSec int
	Env            map[string]string
	PatchPath      string
	PatchRequired  bool
	OutputPaths    []string
	Capabilities   []string
}

type SpecBuilder func(SpecParams) domain.AgentSpec

func BaseSpec(params SpecParams) domain.AgentSpec {
	profile := strings.TrimSpace(params.Profile)
	return domain.AgentSpec{
		Name:           params.Name,
		Profile:        profile,
		ApprovalPolicy: params.ApprovalPolicy,
		Capabilities:   copyStrings(params.Capabilities),
	}
}

func RuntimeSpec(params SpecParams) *domain.AgentRuntimeSpec {
	profile := strings.TrimSpace(params.Profile)
	runtime := &domain.AgentRuntimeSpec{
		Type:           params.RuntimeType,
		Image:          params.Image,
		Command:        ShellCommand(params.Command),
		Workdir:        params.Workdir,
		User:           strings.TrimSpace(params.User),
		TimeoutSec:     params.TimeoutSec,
		MaxTurns:       params.MaxTurns,
		OutputFormat:   params.OutputFormat,
		AllowedTools:   copyStrings(params.AllowedTools),
		IdleTimeoutSec: params.IdleTimeoutSec,
		Env:            copyEnv(params.Env),
		Profile:        profile,
		Prompt:         DefaultPromptSpec(),
		Session:        DefaultSessionSpec(),
		Capabilities:   copyStrings(params.Capabilities),
		Artifacts:      ArtifactPolicy(params),
	}
	if params.RuntimeType == domain.AgentRuntimeTypeAgentImage {
		runtime.MountTarget = agentbundle.MountTarget(params.Name)
		runtime.BinDir = agentbundle.BinDir(runtime.MountTarget)
	}
	return runtime
}

func ArtifactPolicy(params SpecParams) *domain.ArtifactPolicySpec {
	return &domain.ArtifactPolicySpec{
		PatchPath:     PatchPathOrDefault(params.PatchPath),
		PatchRequired: params.PatchRequired,
		OutputPaths:   copyStrings(params.OutputPaths),
		CaptureStdout: true,
		CaptureStderr: true,
		CaptureRawLog: true,
	}
}

func DefaultPromptSpec() *domain.PromptSpec {
	return &domain.PromptSpec{
		Source: domain.PromptSourceInstruction,
		Rounds: []domain.PromptRoundSpec{
			{Index: 1, Source: domain.PromptSourceInstruction},
		},
	}
}

func DefaultSessionSpec() *domain.AgentSessionSpec {
	return &domain.AgentSessionSpec{Mode: domain.AgentSessionModeCreate}
}

func PatchPathOrDefault(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		return path
	}
	return "/tmp/solution.patch"
}

func ShellCommand(command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}
	return []string{"/bin/sh", "-lc", command}
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func copyEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	copied := make(map[string]string, len(env))
	for key, value := range env {
		copied[key] = value
	}
	return copied
}
