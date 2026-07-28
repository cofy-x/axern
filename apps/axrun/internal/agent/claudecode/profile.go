package claudecode

import (
	"fmt"
	"net/url"

	agentpkg "github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

type Profile struct {
	Name         string
	Upstream     *url.URL
	Token        string
	HaikuModel   string
	SonnetModel  string
	OpusModel    string
	APITimeoutMS string
}

func loadProfile(configPath string, profileName string) (Profile, error) {
	name, stored, ok, err := agentprofile.Resolve(configPath, profileName)
	if err != nil {
		return Profile{}, err
	}
	if !ok {
		return Profile{}, fmt.Errorf("claude-code profile %q not found%s", name, agentpkg.ConfigPathHint(configPath))
	}
	if stored.Agent != agentprofile.AgentClaudeCode {
		return Profile{}, fmt.Errorf("claude-code profile %q must use agent %q, got %q", stored.Name, agentprofile.AgentClaudeCode, stored.Agent)
	}
	if stored.ProviderType != agentpkg.ProviderAnthropic {
		return Profile{}, fmt.Errorf("claude-code profile %q must use provider %q, got %q", stored.Name, agentpkg.ProviderAnthropic, stored.ProviderType)
	}
	if err := agentprofile.ValidateWireAPI(stored.Agent, stored.WireAPI); err != nil {
		return Profile{}, fmt.Errorf("claude-code profile %q: %w", stored.Name, err)
	}
	return Profile{
		Name:         stored.Name,
		Upstream:     stored.Upstream,
		Token:        stored.Token,
		HaikuModel:   stored.Config["haiku_model"],
		SonnetModel:  stored.Config["sonnet_model"],
		OpusModel:    stored.Config["opus_model"],
		APITimeoutMS: stored.Config["api_timeout_ms"],
	}, nil
}

func convertProfile(stored agentprofile.Profile) (Profile, error) {
	if stored.Agent != agentprofile.AgentClaudeCode {
		return Profile{}, fmt.Errorf("claude-code profile %q must use agent %q, got %q", stored.Name, agentprofile.AgentClaudeCode, stored.Agent)
	}
	if stored.ProviderType != agentpkg.ProviderAnthropic {
		return Profile{}, fmt.Errorf("claude-code profile %q must use provider %q, got %q", stored.Name, agentpkg.ProviderAnthropic, stored.ProviderType)
	}
	if err := agentprofile.ValidateWireAPI(stored.Agent, stored.WireAPI); err != nil {
		return Profile{}, fmt.Errorf("claude-code profile %q: %w", stored.Name, err)
	}
	return Profile{
		Name:         stored.Name,
		Upstream:     stored.Upstream,
		Token:        stored.Token,
		HaikuModel:   stored.Config["haiku_model"],
		SonnetModel:  stored.Config["sonnet_model"],
		OpusModel:    stored.Config["opus_model"],
		APITimeoutMS: stored.Config["api_timeout_ms"],
	}, nil
}
