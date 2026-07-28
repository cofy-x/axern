package codex

import (
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	codexagent "github.com/cofy-x/axern/apps/axrun/internal/agent/codex"
	rolloutagents "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

const Name = "codex"

func Registration() agent.Registration {
	return RegistrationWithProfiles(nil)
}
func RegistrationWithProfiles(profiles map[string]agentprofile.Profile) agent.Registration {
	return agent.Registration{
		Name:     Name,
		Provider: agent.ProviderOpenAI,
		Kind:     agent.RegistrationKindManaged,
		SupportedRuntimes: []domain.AgentRuntimeType{
			domain.AgentRuntimeTypeSandboxCommand,
			domain.AgentRuntimeTypeAgentImage,
		},
		InstallStrategy:         agent.InstallPreinstalled,
		RequiredCapabilities:    []string{"shell", "file-edit", "patch", "artifact"},
		ProfileRequiredRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeAgentImage},
		HarnessFactory: func(spec domain.AgentSpec) (agent.Harness, error) {
			config, err := codexagent.ConfigFromEnv()
			if err != nil {
				return nil, err
			}
			config.Profiles = profiles
			return codexagent.NewWithConfig(config), nil
		},
	}
}

func BuildSpec(params rolloutagents.SpecParams) domain.AgentSpec {
	params.Profile = strings.TrimSpace(params.Profile)
	params.Name = Name
	spec := rolloutagents.BaseSpec(params)
	runtime := rolloutagents.RuntimeSpec(params)
	spec.Runtime = runtime
	return spec
}
