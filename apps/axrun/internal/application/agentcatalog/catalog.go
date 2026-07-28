package agentcatalog

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/agent/oracle"
	rolloutclaudecode "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents/claudecode"
	rolloutcodex "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents/codex"
	rolloutcommand "github.com/cofy-x/axern/apps/axrun/internal/application/rollout/agents/command"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

// DefaultRegistry returns the built-in agent registry with all known agent
// registrations. It is a pure function with no I/O.
func DefaultRegistry() *agent.Registry {
	return RegistryWithProfiles(nil)
}

func RegistryWithProfiles(profiles map[string]agentprofile.Profile) *agent.Registry {
	r := agent.NewRegistry()
	mustRegister(r, rolloutclaudecode.RegistrationWithProfiles(profiles))
	mustRegister(r, rolloutcodex.RegistrationWithProfiles(profiles))
	mustRegister(r, rolloutcommand.Registration())
	mustRegister(r, agent.Registration{
		Name:                 "oracle",
		Provider:             agent.ProviderNone,
		Kind:                 agent.RegistrationKindBuiltin,
		SupportedRuntimes:    []domain.AgentRuntimeType{domain.AgentRuntimeTypeOracle},
		DefaultRuntimeType:   domain.AgentRuntimeTypeOracle,
		RunByDefault:         true,
		InstallStrategy:      agent.InstallPreinstalled,
		RequiredCapabilities: []string{"shell", "file-edit"},
		HarnessFactory: func(spec domain.AgentSpec) (agent.Harness, error) {
			return oracle.New(), nil
		},
	})
	mustRegister(r, agent.Registration{
		Name:              "noop",
		Provider:          agent.ProviderNone,
		Kind:              agent.RegistrationKindBuiltin,
		SupportedRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeSandboxCommand},
		RunByDefault:      true,
		InstallStrategy:   agent.InstallPreinstalled,
		HarnessFactory: func(spec domain.AgentSpec) (agent.Harness, error) {
			return agent.NoopHarness{}, nil
		},
	})
	return r
}

func mustRegister(registry *agent.Registry, registration agent.Registration) {
	if err := registry.Register(registration); err != nil {
		panic(fmt.Sprintf("register built-in agent %q: %v", registration.Name, err))
	}
}
