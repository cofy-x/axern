package local

import (
	"context"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/rollout"
	sandboxlocal "github.com/cofy-x/axern/apps/axrun/internal/sandbox/local"
)

type Adapter struct {
	Now       func() time.Time
	AgentName string
	Registry  *agent.Registry
}

var _ backend.AgentPreflight = Adapter{}
var _ backend.ProviderPreflight = Adapter{}
var _ backend.ProviderProfilePreflight = Adapter{}

func (a Adapter) Preflight() error {
	if a.Registry == nil || !a.Registry.IsKnown(a.AgentName) {
		return nil
	}
	h, err := a.Registry.NewHarness(a.AgentName, domain.AgentSpec{Name: a.AgentName})
	if err != nil {
		return err
	}
	if h != nil {
		return h.Preflight()
	}
	return nil
}

func (a Adapter) PreflightAgent(agent domain.AgentSpec) error {
	return backend.ValidateAgentRuntimeSupport(string(backend.NameLocal), agent)
}

func (a Adapter) PreflightProvider(ctx context.Context, agentSpec domain.AgentSpec, model domain.ModelSpec) error {
	harness, err := a.agentHarness(agentSpec)
	if err != nil {
		return err
	}
	return backend.PreflightHarnessProvider(ctx, harness, agentSpec, model)
}

func (a Adapter) PreflightProviderProfile(agentSpec domain.AgentSpec) error {
	harness, err := a.agentHarness(agentSpec)
	if err != nil {
		return err
	}
	return backend.PreflightHarnessProfile(harness, agentSpec)
}

func (a Adapter) Execute(request backend.ExecuteRequest) (domain.Episode, error) {
	if err := a.PreflightAgent(request.Episode.Agent); err != nil {
		return request.Episode, err
	}
	if a.Registry != nil {
		if err := a.Registry.ValidateAgent(a.AgentName, request.Episode.Agent); err != nil {
			return request.Episode, err
		}
	}
	agentHarness, err := a.agentHarness(request.Episode.Agent)
	if err != nil {
		return request.Episode, err
	}
	return rollout.Execute(rollout.Request{
		Store:          request.Store,
		Task:           request.Task,
		Episode:        request.Episode,
		Paths:          request.Paths,
		SandboxRuntime: sandboxlocal.Runtime{},
		AgentHarness:   agentHarness,
		Now:            a.Now,
		RuntimeName:    "local",
		PhaseReporter:  request.PhaseReporter,
	})
}

func (a Adapter) agentHarness(spec domain.AgentSpec) (agent.Harness, error) {
	if a.Registry == nil || !a.Registry.IsKnown(spec.Name) {
		return nil, nil
	}
	reg, _ := a.Registry.Lookup(spec.Name)
	if reg.HarnessFactory == nil {
		return nil, nil
	}
	return reg.HarnessFactory(spec)
}
