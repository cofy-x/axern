package claudecode

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
	stored, err := agent.ResolveProfile(h.Config.Profiles, h.Config.ConfigPath, profile.Name)
	if err != nil {
		return agentprofile.ProbeResult{}, err
	}
	if stored.Agent != agentprofile.AgentClaudeCode || stored.ProviderType != agent.ProviderAnthropic {
		return agentprofile.ProbeResult{}, fmt.Errorf("claude-code profile %q must use agent %q and provider %q", stored.Name, agentprofile.AgentClaudeCode, agent.ProviderAnthropic)
	}
	return agentprofile.Probe(ctx, agentprofile.ProbeRequest{Profile: stored, Model: model.ID})
}

func (h *Harness) writeRemoteConfig(_ agent.Request, plan *agent.LaunchPlan) error {
	if strings.TrimSpace(plan.Profile) == "" {
		return nil
	}
	profile, err := h.resolveProfile(plan.Profile)
	if err != nil {
		return err
	}
	script, err := writeRemoteClaudeConfig(profile)
	if err != nil {
		return err
	}
	plan.Env["ANTHROPIC_API_KEY"] = profile.Token
	plan.Command = agent.WrapCommandWithShellPrelude(script, plan.Command)
	return nil
}

func (h *Harness) resolveProfile(profileName string) (Profile, error) {
	if stored, ok := h.Config.Profiles[strings.TrimSpace(profileName)]; ok {
		return convertProfile(stored)
	}
	return loadProfile(h.Config.ConfigPath, profileName)
}
