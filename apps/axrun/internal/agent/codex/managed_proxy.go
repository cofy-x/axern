package codex

import (
	"context"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

func (h *Harness) ProbeProvider(ctx context.Context, agentSpec domain.AgentSpec, model domain.ModelSpec) (agentprofile.ProbeResult, error) {
	profileName := h.profileName(agentSpec)
	profile, err := h.resolveProfile(profileName)
	if err != nil {
		return agentprofile.ProbeResult{}, err
	}
	if profile.Agent != agentprofile.AgentCodex || profile.ProviderType != agent.ProviderOpenAI {
		return agentprofile.ProbeResult{}, fmt.Errorf("codex profile %q must use agent %q and provider %q", profile.Name, agentprofile.AgentCodex, agent.ProviderOpenAI)
	}
	return agentprofile.Probe(ctx, agentprofile.ProbeRequest{Profile: profile, Model: model.ID})
}

func (h *Harness) ManagedProxyConfig(agentSpec domain.AgentSpec) (*agent.ManagedProxyConfig, error) {
	profileName := h.profileName(agentSpec)
	if strings.TrimSpace(profileName) == "" {
		return nil, nil
	}
	profile, err := h.resolveProfile(profileName)
	if err != nil {
		return nil, err
	}
	if profile.ProviderType != agent.ProviderOpenAI {
		return nil, fmt.Errorf("codex profile %q must use provider %q, got %q", profile.Name, agent.ProviderOpenAI, profile.ProviderType)
	}
	if err := agentprofile.ValidateWireAPI(profile.Agent, profile.WireAPI); err != nil {
		return nil, fmt.Errorf("codex profile %q: %w", profile.Name, err)
	}
	return &agent.ManagedProxyConfig{
		Upstream:     profile.Upstream,
		Token:        profile.Token,
		ProviderType: profile.ProviderType,
	}, nil
}

// writeRemoteConfig prepares Codex CLI provider config so Codex uses the
// recording proxy for API calls. Agent-image runtimes receive the setup as a
// command prelude so it runs inside the task sandbox with the mounted bundle.
func (h *Harness) writeRemoteConfig(request agent.Request, plan *agent.LaunchPlan) error {
	if request.ManagedProxy == nil {
		return nil
	}
	profile, err := h.resolveProfile(plan.Profile)
	if err != nil {
		return err
	}
	script, err := writeRemoteCodexConfig(profile)
	if err != nil {
		return err
	}
	for key, value := range profile.Env {
		if _, exists := plan.Env[key]; !exists {
			plan.Env[key] = value
		}
	}
	plan.Command = agent.WrapCommandWithShellPrelude(script, plan.Command)
	return nil
}

func (h *Harness) resolveProfile(profileName string) (agent.Profile, error) {
	return agent.ResolveProfile(h.Config.Profiles, h.Config.ConfigPath, profileName)
}
