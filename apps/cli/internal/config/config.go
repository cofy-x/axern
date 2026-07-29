package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/cofy-x/axern/lib/go/agentprofile"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

const (
	DefaultDirName  = "axern"
	DefaultFileName = "config.json"
)

type File struct {
	CurrentContext string                           `json:"current_context,omitempty"`
	Contexts       map[string]*clientconfig.Context `json:"contexts,omitempty"`
	AgentProfiles  agentprofile.ProfilesConfig      `json:"agent_profiles,omitempty"`
}

func DefaultPath() string {
	return clientconfig.DefaultPath()
}

func Load(path string) (*File, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{Contexts: map[string]*clientconfig.Context{}}, nil
	}
	if err != nil {
		return nil, err
	}
	cfg := &File{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parse axern config %q: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse axern config %q: multiple JSON values", path)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*clientconfig.Context{}
	}
	for name, context := range cfg.Contexts {
		if err := clientconfig.Validate(context); err != nil {
			return nil, fmt.Errorf("invalid axern context %q: %w", name, err)
		}
	}
	ensureAgentProfiles(cfg)
	return cfg, nil
}

func Save(path string, cfg *File) error {
	if path == "" {
		path = DefaultPath()
	}
	if cfg == nil {
		cfg = &File{}
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*clientconfig.Context{}
	}
	ensureAgentProfiles(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func ResolveAgentProfile(path, profileName string) (string, agentprofile.Profile, bool, error) {
	cfg, err := Load(path)
	if err != nil {
		return "", agentprofile.Profile{}, false, err
	}
	return agentprofile.ResolveFromConfig(&agentprofile.ConfigFile{AgentProfiles: cfg.AgentProfiles}, profileName)
}

func AgentProfileNames(cfg *File) []string {
	return agentprofile.ProfileNames(&agentprofile.ConfigFile{AgentProfiles: cfg.AgentProfiles})
}

func Resolve(path, contextName string) (string, *clientconfig.Context, bool, error) {
	cfg, err := Load(path)
	if err != nil {
		return "", nil, false, err
	}
	name := contextName
	if name == "" {
		name = cfg.CurrentContext
	}
	if name == "" {
		return "", nil, false, nil
	}
	ctx, ok := cfg.Contexts[name]
	if !ok || ctx == nil {
		return "", nil, false, fmt.Errorf("axern context %q not found in %s", name, resolvedPath(path))
	}
	return name, ctx, true, nil
}

func ContextNames(cfg *File) []string {
	if cfg == nil || len(cfg.Contexts) == 0 {
		return nil
	}
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func resolvedPath(path string) string {
	if path != "" {
		return path
	}
	return DefaultPath()
}

func ensureAgentProfiles(cfg *File) {
	if cfg == nil {
		return
	}
	if cfg.AgentProfiles.Profiles == nil {
		cfg.AgentProfiles.Profiles = map[string]*agentprofile.ProfileConfig{}
	}
}
