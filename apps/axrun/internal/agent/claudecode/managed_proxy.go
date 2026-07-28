package claudecode

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
	stored, err := agent.ResolveProfile(h.Config.Profiles, h.Config.ConfigPath, profileName)
	if err != nil {
		return agentprofile.ProbeResult{}, err
	}
	if stored.Agent != agentprofile.AgentClaudeCode || stored.ProviderType != agent.ProviderAnthropic {
		return agentprofile.ProbeResult{}, fmt.Errorf("claude-code profile %q must use agent %q and provider %q", stored.Name, agentprofile.AgentClaudeCode, agent.ProviderAnthropic)
	}
	return agentprofile.Probe(ctx, agentprofile.ProbeRequest{Profile: stored, Model: model.ID})
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
	return &agent.ManagedProxyConfig{
		Upstream:     profile.Upstream,
		Token:        profile.Token,
		ProviderType: agent.ProviderAnthropic,
	}, nil
}

// writeRemoteConfig prepares the Claude-specific settings/config files so
// claude-code uses the recording proxy for API calls. Agent-image runtimes
// receive the setup as a command prelude so it runs inside the task sandbox
// with the mounted bundle.
func (h *Harness) writeRemoteConfig(request agent.Request, plan *agent.LaunchPlan) error {
	if request.ManagedProxy == nil {
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
	plan.Command = agent.WrapCommandWithShellPrelude(script, plan.Command)
	return nil
}

func (h *Harness) resolveProfile(profileName string) (Profile, error) {
	stored, ok := h.Config.Profiles[strings.TrimSpace(profileName)]
	if ok {
		return convertProfile(stored)
	}
	return loadProfile(h.Config.ConfigPath, profileName)
}
