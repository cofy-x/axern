package codex

import (
	"context"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/agentprofile"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func (h *Harness) ProbeProvider(ctx context.Context, spec domain.AgentSpec, model domain.ModelSpec) (agentprofile.ProbeResult, error) {
	profile, err := h.resolveProfile(h.profileName(spec))
	if err != nil {
		return agentprofile.ProbeResult{}, err
	}
	if profile.Agent != agentprofile.AgentCodex || profile.ProviderType != agent.ProviderOpenAI {
		return agentprofile.ProbeResult{}, fmt.Errorf("codex profile %q must use agent %q and provider %q", profile.Name, agentprofile.AgentCodex, agent.ProviderOpenAI)
	}
	return agentprofile.Probe(ctx, agentprofile.ProbeRequest{Profile: profile, Model: model.ID})
}

func (h *Harness) writeRemoteConfig(_ agent.Request, plan *agent.LaunchPlan) error {
	if strings.TrimSpace(plan.Profile) == "" {
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
	plan.Env["OPENAI_API_KEY"] = profile.Token
	plan.Command = agent.WrapCommandWithShellPrelude(script, plan.Command)
	return nil
}

func (h *Harness) resolveProfile(profileName string) (agent.Profile, error) {
	return agent.ResolveProfile(h.Config.Profiles, h.Config.ConfigPath, profileName)
}
