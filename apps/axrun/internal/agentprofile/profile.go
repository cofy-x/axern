package agentprofile

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
)

const (
	DefaultProfileName = "default"

	ProviderOpenAI    ProviderType = "openai"
	ProviderAnthropic ProviderType = "anthropic"

	WireAPIResponses         WireAPI = "responses"
	WireAPIAnthropicMessages WireAPI = "anthropic_messages"

	AgentCodex      AgentType = "codex"
	AgentClaudeCode AgentType = "claude-code"
)

type AgentType string

type ProviderType string

type WireAPI string

type Profile struct {
	Name          string
	Agent         AgentType
	ProviderType  ProviderType
	WireAPI       WireAPI
	Upstream      *url.URL
	Token         string
	TemplateID    string
	Namespace     string
	RemoteUser    string
	RestoreOnExit bool
	Env           map[string]string
	Config        map[string]string
}

type ConfigFile struct {
	AgentProfiles ProfilesConfig `json:"agent_profiles,omitempty"`
}

type ProfilesConfig struct {
	CurrentProfile string                    `json:"current_profile,omitempty"`
	Profiles       map[string]*ProfileConfig `json:"profiles,omitempty"`
}

type ProfileConfig struct {
	Agent         string            `json:"agent,omitempty"`
	Provider      string            `json:"provider,omitempty"`
	WireAPI       string            `json:"wire_api,omitempty"`
	Upstream      string            `json:"upstream,omitempty"`
	Token         string            `json:"token,omitempty"`
	TemplateID    string            `json:"template_id,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	RemoteUser    string            `json:"remote_user,omitempty"`
	RestoreOnExit bool              `json:"restore_on_exit,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	Config        map[string]string `json:"config,omitempty"`
}

type BehaviorSnapshot struct {
	Agent             string
	Provider          string
	WireAPI           string
	Endpoint          string
	ConfigFingerprint string
}

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "config.json"
	}
	return filepath.Join(home, ".config", "axrun", "config.json")
}

func Load(path string) (*ConfigFile, string, error) {
	path = ResolvePath(path)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, path, fmt.Errorf("inspect axrun config: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, path, fmt.Errorf("axrun config %q must not be a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return nil, path, fmt.Errorf("axrun config %q must be a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, path, fmt.Errorf("read axrun config: %w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, path, fmt.Errorf("inspect opened axrun config: %w", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, path, fmt.Errorf("axrun config %q changed while it was being opened", path)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		_ = file.Close()
		return nil, path, fmt.Errorf("read axrun config: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, path, fmt.Errorf("close axrun config: %w", err)
	}
	cfg := &ConfigFile{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, path, fmt.Errorf("parse axrun config %q: %w", path, err)
	}
	if containsPlaintextToken(cfg) && openedInfo.Mode().Perm()&0o077 != 0 {
		return nil, path, fmt.Errorf("axrun config %q contains a plaintext token and must use owner-only permissions (for example 0600)", path)
	}
	Ensure(cfg)
	return cfg, path, nil
}

func containsPlaintextToken(cfg *ConfigFile) bool {
	if cfg == nil {
		return false
	}
	for _, profile := range cfg.AgentProfiles.Profiles {
		if profile != nil && strings.TrimSpace(profile.Token) != "" {
			return true
		}
	}
	return false
}

func Snapshot(profile Profile) (BehaviorSnapshot, error) {
	if profile.Upstream == nil {
		return BehaviorSnapshot{}, fmt.Errorf("agent profile %q upstream is required", profile.Name)
	}
	behavior := struct {
		Agent         AgentType         `json:"agent"`
		Provider      ProviderType      `json:"provider"`
		WireAPI       WireAPI           `json:"wire_api"`
		Endpoint      string            `json:"endpoint"`
		TemplateID    string            `json:"template_id,omitempty"`
		Namespace     string            `json:"namespace,omitempty"`
		RemoteUser    string            `json:"remote_user,omitempty"`
		RestoreOnExit bool              `json:"restore_on_exit,omitempty"`
		Env           map[string]string `json:"env,omitempty"`
		Config        map[string]string `json:"config,omitempty"`
	}{
		Agent: profile.Agent, Provider: profile.ProviderType, WireAPI: profile.WireAPI,
		Endpoint: profile.Upstream.String(), TemplateID: profile.TemplateID,
		Namespace: profile.Namespace, RemoteUser: profile.RemoteUser,
		RestoreOnExit: profile.RestoreOnExit, Env: profile.Env, Config: profile.Config,
	}
	data, err := json.Marshal(behavior)
	if err != nil {
		return BehaviorSnapshot{}, err
	}
	digest := sha256.Sum256(data)
	return BehaviorSnapshot{
		Agent: string(profile.Agent), Provider: string(profile.ProviderType),
		WireAPI: string(profile.WireAPI), Endpoint: profile.Upstream.String(),
		ConfigFingerprint: fmt.Sprintf("sha256:%x", digest),
	}, nil
}

func Resolve(path string, profileName string) (string, Profile, bool, error) {
	cfg, _, err := Load(path)
	if err != nil {
		return "", Profile{}, false, err
	}
	return ResolveFromConfig(cfg, profileName)
}

func ResolveFromConfig(cfg *ConfigFile, profileName string) (string, Profile, bool, error) {
	Ensure(cfg)
	name := strings.TrimSpace(profileName)
	if name == "" {
		name = strings.TrimSpace(cfg.AgentProfiles.CurrentProfile)
	}
	if name == "" {
		name = DefaultProfileName
	}
	stored := cfg.AgentProfiles.Profiles[name]
	if stored == nil {
		return name, Profile{}, false, nil
	}
	profile, err := ParseProfile(name, stored)
	if err != nil {
		return name, Profile{}, false, err
	}
	return name, profile, true, nil
}

func ParseProfile(name string, stored *ProfileConfig) (Profile, error) {
	if stored == nil {
		return Profile{}, fmt.Errorf("agent profile %q is empty", name)
	}
	agentType, err := ParseAgentType(stored.Agent)
	if err != nil {
		return Profile{}, fmt.Errorf("agent profile %q: %w", name, err)
	}
	providerType, err := ParseProviderType(stored.Provider)
	if err != nil {
		return Profile{}, fmt.Errorf("agent profile %q: %w", name, err)
	}
	if err := ValidateProvider(agentType, providerType); err != nil {
		return Profile{}, fmt.Errorf("agent profile %q: %w", name, err)
	}
	wireAPI, err := ParseWireAPI(stored.WireAPI)
	if err != nil {
		return Profile{}, fmt.Errorf("agent profile %q: %w", name, err)
	}
	if err := ValidateWireAPI(agentType, wireAPI); err != nil {
		return Profile{}, fmt.Errorf("agent profile %q: %w", name, err)
	}
	upstream, err := ParseUpstream("agent profile", stored.Upstream)
	if err != nil {
		return Profile{}, err
	}
	token := strings.TrimSpace(stored.Token)
	if token == "" {
		return Profile{}, fmt.Errorf("agent profile %q does not define token", name)
	}
	for key := range stored.Env {
		if contract.IsSensitiveEnvKey(key) {
			return Profile{}, fmt.Errorf("agent profile %q env %q is credential-like; use the dedicated token field", name, key)
		}
	}
	for key := range stored.Config {
		if contract.IsSensitiveEnvKey(key) {
			return Profile{}, fmt.Errorf("agent profile %q config %q is credential-like; use the dedicated token field", name, key)
		}
	}
	return Profile{
		Name:          strings.TrimSpace(name),
		Agent:         agentType,
		ProviderType:  providerType,
		WireAPI:       wireAPI,
		Upstream:      upstream,
		Token:         token,
		TemplateID:    strings.TrimSpace(stored.TemplateID),
		Namespace:     strings.TrimSpace(stored.Namespace),
		RemoteUser:    strings.TrimSpace(stored.RemoteUser),
		RestoreOnExit: stored.RestoreOnExit,
		Env:           CopyMap(stored.Env),
		Config:        CopyMap(stored.Config),
	}, nil
}

func ParseWireAPI(raw string) (WireAPI, error) {
	switch WireAPI(strings.TrimSpace(raw)) {
	case WireAPIResponses:
		return WireAPIResponses, nil
	case WireAPIAnthropicMessages:
		return WireAPIAnthropicMessages, nil
	case "":
		return "", fmt.Errorf("wire_api is required")
	default:
		return "", fmt.Errorf("unsupported wire_api %q", raw)
	}
}

func RequiredWireAPI(agent AgentType) (WireAPI, error) {
	switch agent {
	case AgentCodex:
		return WireAPIResponses, nil
	case AgentClaudeCode:
		return WireAPIAnthropicMessages, nil
	default:
		return "", fmt.Errorf("unsupported agent %q", agent)
	}
}

func ValidateWireAPI(agent AgentType, wireAPI WireAPI) error {
	required, err := RequiredWireAPI(agent)
	if err != nil {
		return err
	}
	if wireAPI != required {
		return fmt.Errorf("agent %q requires wire_api %q, got %q", agent, required, wireAPI)
	}
	return nil
}

func ParseAgentType(raw string) (AgentType, error) {
	switch AgentType(strings.TrimSpace(raw)) {
	case AgentCodex:
		return AgentCodex, nil
	case AgentClaudeCode:
		return AgentClaudeCode, nil
	default:
		return "", fmt.Errorf("unsupported agent %q", raw)
	}
}

func ParseProviderType(raw string) (ProviderType, error) {
	switch ProviderType(strings.TrimSpace(raw)) {
	case ProviderOpenAI:
		return ProviderOpenAI, nil
	case ProviderAnthropic:
		return ProviderAnthropic, nil
	default:
		return "", fmt.Errorf("unsupported provider %q", raw)
	}
}

func ValidateProvider(agent AgentType, provider ProviderType) error {
	var required ProviderType
	switch agent {
	case AgentCodex:
		required = ProviderOpenAI
	case AgentClaudeCode:
		required = ProviderAnthropic
	default:
		return fmt.Errorf("unsupported agent %q", agent)
	}
	if provider != required {
		return fmt.Errorf("agent %q requires provider %q, got %q", agent, required, provider)
	}
	return nil
}

func ParseUpstream(label string, raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s upstream is required", label)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse %s upstream: %w", label, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s upstream must use http:// or https://", label)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return nil, fmt.Errorf("%s upstream must include a host", label)
	}
	if parsed.User != nil {
		return nil, fmt.Errorf("%s upstream must not include user credentials", label)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s upstream must not include a query or fragment", label)
	}
	return parsed, nil
}

func ProfileNames(cfg *ConfigFile) []string {
	if cfg == nil || len(cfg.AgentProfiles.Profiles) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.AgentProfiles.Profiles))
	for name := range cfg.AgentProfiles.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Ensure(cfg *ConfigFile) {
	if cfg == nil {
		return
	}
	if cfg.AgentProfiles.Profiles == nil {
		cfg.AgentProfiles.Profiles = map[string]*ProfileConfig{}
	}
}

func ResolvePath(path string) string {
	if strings.TrimSpace(path) != "" {
		return strings.TrimSpace(path)
	}
	if env := strings.TrimSpace(os.Getenv("AXRUN_CONFIG")); env != "" {
		return env
	}
	return DefaultPath()
}

func CopyMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	copied := make(map[string]string, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func ProbeModel(profile Profile) string {
	for _, key := range []string{"model", "sonnet_model", "opus_model", "haiku_model"} {
		if value := strings.TrimSpace(profile.Config[key]); value != "" {
			return value
		}
	}
	return ""
}
