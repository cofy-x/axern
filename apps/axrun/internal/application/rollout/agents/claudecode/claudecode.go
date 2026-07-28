package claudecode

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	claudecodeagent "github.com/cofy-x/axern/apps/axrun/internal/agent/claudecode"
	rolloutagents "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

const Name = "claude-code"

func Registration() agent.Registration {
	return RegistrationWithProfiles(nil)
}
func RegistrationWithProfiles(profiles map[string]agentprofile.Profile) agent.Registration {
	return agent.Registration{
		Name:     Name,
		Provider: agent.ProviderAnthropic,
		Kind:     agent.RegistrationKindManaged,
		SupportedRuntimes: []domain.AgentRuntimeType{
			domain.AgentRuntimeTypeSandboxCommand,
			domain.AgentRuntimeTypeAgentImage,
		},
		InstallStrategy:         agent.InstallPreinstalled,
		RequiredCapabilities:    []string{"shell", "file-edit", "patch", "artifact"},
		ProfileRequiredRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeAgentImage},
		ValidateRuntime:         validateRuntime,
		HarnessFactory: func(spec domain.AgentSpec) (agent.Harness, error) {
			config, err := claudecodeagent.ConfigFromEnv()
			if err != nil {
				return nil, err
			}
			config.Profiles = profiles
			return claudecodeagent.New(config), nil
		},
	}
}

func validateRuntime(spec domain.AgentSpec) error {
	if spec.Runtime == nil || spec.Runtime.Type != domain.AgentRuntimeTypeAgentImage {
		return nil
	}
	artifacts := spec.Runtime.Artifacts
	if artifacts == nil {
		return fmt.Errorf("claude-code agent-image runtime requires artifact policy")
	}
	if !artifacts.CaptureStdout {
		return fmt.Errorf("claude-code agent-image runtime must capture stdout")
	}
	if !artifacts.CaptureStderr {
		return fmt.Errorf("claude-code agent-image runtime must capture stderr")
	}
	if !artifacts.CaptureRawLog {
		return fmt.Errorf("claude-code agent-image runtime must capture raw log")
	}
	return nil
}

func BuildSpec(params rolloutagents.SpecParams) domain.AgentSpec {
	params.Profile = strings.TrimSpace(params.Profile)
	params.Name = Name
	spec := rolloutagents.BaseSpec(params)
	runtime := rolloutagents.RuntimeSpec(params)
	spec.Runtime = runtime
	return spec
}
