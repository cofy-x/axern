package command

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	rolloutagents "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const Name = "command"

func Registration() agent.Registration {
	return agent.Registration{
		Name:               Name,
		Provider:           agent.ProviderNone,
		SupportedRuntimes:  []domain.AgentRuntimeType{domain.AgentRuntimeTypeSandboxCommand},
		InstallStrategy:    agent.InstallPreinstalled,
		Kind:               agent.RegistrationKindCommand,
		RunByDefault:       true,
		DefaultRuntimeType: domain.AgentRuntimeTypeSandboxCommand,
		RequiredCapabilities: []string{
			"shell",
		},
		ValidateRuntime: validateRuntime,
		HarnessFactory: func(domain.AgentSpec) (agent.Harness, error) {
			return agent.CommandHarness{}, nil
		},
	}
}

func BuildSpec(params rolloutagents.SpecParams) domain.AgentSpec {
	params.Name = Name
	params.Profile = ""
	params.ApprovalPolicy = ""
	params.RuntimeType = domain.AgentRuntimeTypeSandboxCommand
	spec := rolloutagents.BaseSpec(params)
	spec.Runtime = rolloutagents.RuntimeSpec(params)
	return spec
}

func validateRuntime(spec domain.AgentSpec) error {
	if spec.Runtime == nil || spec.Runtime.Type != domain.AgentRuntimeTypeSandboxCommand {
		return fmt.Errorf("command agent requires sandbox-command runtime")
	}
	if len(spec.Runtime.Command) == 0 || strings.TrimSpace(spec.Runtime.Command[len(spec.Runtime.Command)-1]) == "" {
		return fmt.Errorf("command agent command is required")
	}
	return nil
}
