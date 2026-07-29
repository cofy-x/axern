// Package clientconfig loads the explicit context file shared by the Axern CLI and SDKs.
package clientconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ProxyModeEnv    = "env"
	ProxyModeDirect = "direct"
)

type TLS struct {
	CACert     string `json:"ca_cert,omitempty"`
	Cert       string `json:"cert,omitempty"`
	Key        string `json:"key,omitempty"`
	ServerName string `json:"server_name,omitempty"`
}

type Context struct {
	Endpoint        string `json:"endpoint"`
	ServiceURL      string `json:"service_url,omitempty"`
	SSHEndpoint     string `json:"ssh_endpoint,omitempty"`
	SSHIdentityFile string `json:"ssh_identity_file,omitempty"`
	TLS             TLS    `json:"tls"`
	ProxyMode       string `json:"proxy_mode,omitempty"`
}

type File struct {
	CurrentContext string              `json:"current_context,omitempty"`
	Contexts       map[string]*Context `json:"contexts,omitempty"`
	AgentProfiles  json.RawMessage     `json:"agent_profiles,omitempty"`
}

func DefaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "config.json"
	}
	return filepath.Join(home, ".config", "axern", "config.json")
}

func Load(path string) (*File, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &File{Contexts: map[string]*Context{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg File
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse axern config %q: %w", path, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse axern config %q: multiple JSON values", path)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*Context{}
	}
	for name, context := range cfg.Contexts {
		if err := Validate(context); err != nil {
			return nil, fmt.Errorf("invalid axern context %q: %w", name, err)
		}
	}
	return &cfg, nil
}

func Resolve(path, name string) (string, *Context, bool, error) {
	cfg, err := Load(path)
	if err != nil {
		return "", nil, false, err
	}
	if name == "" {
		name = cfg.CurrentContext
	}
	if name == "" {
		return "", nil, false, nil
	}
	context, ok := cfg.Contexts[name]
	if !ok || context == nil {
		return "", nil, false, fmt.Errorf("axern context %q not found in %s", name, resolvedPath(path))
	}
	return name, context, true, nil
}

func Validate(context *Context) error {
	if context == nil {
		return fmt.Errorf("context is required")
	}
	if strings.TrimSpace(context.Endpoint) == "" {
		return fmt.Errorf("endpoint is required")
	}
	if strings.TrimSpace(context.TLS.CACert) == "" || strings.TrimSpace(context.TLS.Cert) == "" || strings.TrimSpace(context.TLS.Key) == "" {
		return fmt.Errorf("tls.ca_cert, tls.cert, and tls.key are required")
	}
	switch strings.TrimSpace(context.ProxyMode) {
	case "", ProxyModeEnv, ProxyModeDirect:
		return nil
	default:
		return fmt.Errorf("proxy_mode must be %q or %q", ProxyModeEnv, ProxyModeDirect)
	}
}

func ContextNames(cfg *File) []string {
	if cfg == nil {
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
