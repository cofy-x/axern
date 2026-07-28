package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cofy-x/axern/lib/go/agentprofile"
)

type Adapter interface {
	Agent() agentprofile.AgentType
	Provider() agentprofile.ProviderType
	DefaultTemplateID() string
	BundleID() string
	SessionEnv(remotePort int32, profile agentprofile.Profile, localToken string) map[string]string
	RunCommand(binary string, args []string) (string, bool)
	Validate(agentprofile.Profile) error
}

func AdapterFor(agentType agentprofile.AgentType) (Adapter, error) {
	switch agentType {
	case agentprofile.AgentClaudeCode:
		return claudeCodeAdapter{}, nil
	case agentprofile.AgentCodex:
		return codexAdapter{}, nil
	default:
		return nil, fmt.Errorf("unsupported agent %q", agentType)
	}
}

type claudeCodeAdapter struct{}

func (claudeCodeAdapter) Agent() agentprofile.AgentType       { return agentprofile.AgentClaudeCode }
func (claudeCodeAdapter) Provider() agentprofile.ProviderType { return agentprofile.ProviderAnthropic }
func (claudeCodeAdapter) DefaultTemplateID() string           { return "coding-base" }
func (claudeCodeAdapter) BundleID() string                    { return "claude-code" }
func (claudeCodeAdapter) SessionEnv(remotePort int32, profile agentprofile.Profile, localToken string) map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       fmt.Sprintf("http://127.0.0.1:%d", remotePort),
		"ANTHROPIC_API_KEY":                        localToken,
		"CLAUDE_CODE_SIMPLE":                       "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"NO_PROXY":                                 "127.0.0.1,localhost",
		"no_proxy":                                 "127.0.0.1,localhost",
	}
	for _, item := range []struct {
		key string
		env string
	}{
		{"haiku_model", "ANTHROPIC_DEFAULT_HAIKU_MODEL"},
		{"sonnet_model", "ANTHROPIC_DEFAULT_SONNET_MODEL"},
		{"opus_model", "ANTHROPIC_DEFAULT_OPUS_MODEL"},
		{"api_timeout_ms", "API_TIMEOUT_MS"},
	} {
		if value := strings.TrimSpace(profile.Config[item.key]); value != "" {
			env[item.env] = value
		}
	}
	return env
}
func (claudeCodeAdapter) RunCommand(binary string, args []string) (string, bool) {
	return agentCommand(binary, args)
}
func (a claudeCodeAdapter) Validate(profile agentprofile.Profile) error {
	return validateAgentProvider(a, profile)
}

type codexAdapter struct{}

func (codexAdapter) Agent() agentprofile.AgentType       { return agentprofile.AgentCodex }
func (codexAdapter) Provider() agentprofile.ProviderType { return agentprofile.ProviderOpenAI }
func (codexAdapter) DefaultTemplateID() string           { return "coding-base" }
func (codexAdapter) BundleID() string                    { return "codex" }
func (codexAdapter) SessionEnv(remotePort int32, _ agentprofile.Profile, localToken string) map[string]string {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", remotePort)
	return map[string]string{
		"OPENAI_BASE_URL": baseURL,
		"OPENAI_API_KEY":  localToken,
		"NO_PROXY":        "127.0.0.1,localhost",
		"no_proxy":        "127.0.0.1,localhost",
	}
}
func (codexAdapter) RunCommand(binary string, args []string) (string, bool) {
	return agentCommand(binary, args)
}
func (a codexAdapter) Validate(profile agentprofile.Profile) error {
	return validateAgentProvider(a, profile)
}

func validateAgentProvider(adapter Adapter, profile agentprofile.Profile) error {
	if profile.Agent != adapter.Agent() {
		return fmt.Errorf("profile %q must use agent %q, got %q", profile.Name, adapter.Agent(), profile.Agent)
	}
	if profile.ProviderType != adapter.Provider() {
		return fmt.Errorf("%s profile %q must use provider %q, got %q", adapter.Agent(), profile.Name, adapter.Provider(), profile.ProviderType)
	}
	if profile.Upstream == nil {
		return fmt.Errorf("%s profile %q upstream is required", adapter.Agent(), profile.Name)
	}
	if strings.TrimSpace(profile.Token) == "" {
		return fmt.Errorf("%s profile %q token is required", adapter.Agent(), profile.Name)
	}
	return agentprofile.ValidateWireAPI(profile.Agent, profile.WireAPI)
}

func agentCommand(binary string, args []string) (string, bool) {
	if len(args) == 0 {
		return shellQuote(binary), true
	}
	parts := []string{shellQuote(binary)}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " "), false
}

func commandWithEnv(env map[string]string, command string) string {
	if len(env) == 0 {
		return command
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return command
	}
	sort.Strings(keys)
	parts := []string{"env"}
	for _, key := range keys {
		parts = append(parts, shellQuote(key+"="+env[key]))
	}
	parts = append(parts, command)
	return strings.Join(parts, " ")
}

func commandInWorkspaceWithEnv(env map[string]string, command string) string {
	return "cd \"" + DefaultWorkspace + "\" && " + commandWithEnv(env, command)
}

func mergeCommandEnv(values ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, values := range values {
		for key, value := range values {
			if strings.TrimSpace(key) != "" {
				merged[key] = value
			}
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-./:=,+", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
