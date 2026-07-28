package agent

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

type ProviderType = agentprofile.ProviderType

const (
	ProviderAnthropic ProviderType = agentprofile.ProviderAnthropic
	ProviderOpenAI    ProviderType = agentprofile.ProviderOpenAI
	ProviderLocal     ProviderType = "local"
	ProviderNone      ProviderType = "none"
)

type InstallStrategy string

type RegistrationKind string

const (
	InstallPreinstalled InstallStrategy = "preinstalled"
	InstallRuntimeImage InstallStrategy = "runtime-install"
	InstallImageOnly    InstallStrategy = "image-only"
)

const (
	RegistrationKindBuiltin RegistrationKind = "builtin"
	RegistrationKindManaged RegistrationKind = "managed"
	RegistrationKindCommand RegistrationKind = "command"
)

// HarnessFactory constructs a Harness for the given agent specification.
// Factories read environment-based configuration internally.
type HarnessFactory func(spec domain.AgentSpec) (Harness, error)

// Registration describes a single agent type.
type Registration struct {
	Name              string
	Provider          ProviderType
	SupportedRuntimes []domain.AgentRuntimeType
	InstallStrategy   InstallStrategy
	DefaultModel      string
	Kind              RegistrationKind
	// DefaultRuntimeType is the runtime type applied when the caller does not
	// provide an explicit runtime selection. If empty, model-level fallback
	// logic applies (e.g. sandbox-command for most agents).
	DefaultRuntimeType      domain.AgentRuntimeType
	RunByDefault            bool
	RequiredCapabilities    []string
	ProfileRequiredRuntimes []domain.AgentRuntimeType
	ValidateRuntime         func(domain.AgentSpec) error
	HarnessFactory          HarnessFactory // nil means agent phase is skipped
}

// Registry maps agent names to their registrations.
type Registry struct {
	entries map[string]Registration
}

func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]Registration)}
}

// Register adds an agent registration. Name and kind are required so a newly
// added managed agent cannot silently inherit builtin approval semantics.
func (r *Registry) Register(reg Registration) error {
	name := strings.TrimSpace(reg.Name)
	if name == "" {
		return fmt.Errorf("agent registration name must not be empty")
	}
	if _, exists := r.entries[name]; exists {
		return fmt.Errorf("agent %q is already registered", name)
	}
	switch reg.Kind {
	case RegistrationKindBuiltin, RegistrationKindManaged, RegistrationKindCommand:
	default:
		return fmt.Errorf("agent %q has unsupported registration kind %q", name, reg.Kind)
	}
	reg.Name = name
	r.entries[name] = reg
	return nil
}

func (reg Registration) IsManaged() bool { return reg.Kind == RegistrationKindManaged }

func (reg Registration) IsCommand() bool { return reg.Kind == RegistrationKindCommand }

// Lookup returns the registration for the given agent name.
func (r *Registry) Lookup(name string) (Registration, bool) {
	reg, ok := r.entries[name]
	return reg, ok
}

// IsKnown reports whether name is a registered agent.
func (r *Registry) IsKnown(name string) bool {
	_, ok := r.entries[name]
	return ok
}

// Names returns all registered agent names in sorted order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.entries))
	for name := range r.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewHarness constructs a harness for the named agent. Returns (nil, nil)
// when the agent is registered but has no factory (agent phase skipped).
// Returns an error when the agent is not registered.
func (r *Registry) NewHarness(name string, spec domain.AgentSpec) (Harness, error) {
	reg, ok := r.entries[name]
	if !ok {
		return nil, fmt.Errorf("unknown agent %q", name)
	}
	if reg.HarnessFactory == nil {
		return nil, nil
	}
	return reg.HarnessFactory(spec)
}

// ValidateAgent checks the agent from the given spec, using the
// overrideName if non-empty. It verifies the agent is registered and
// the runtime type is supported.
func (r *Registry) ValidateAgent(overrideName string, spec domain.AgentSpec) error {
	name := overrideName
	if name == "" {
		name = spec.Name
	}
	runtimeType := domain.AgentRuntimeType("")
	if spec.Runtime != nil {
		runtimeType = spec.Runtime.Type
	}
	if err := r.validateRegistration(name, runtimeType); err != nil {
		return err
	}
	reg := r.entries[name]
	if reg.IsManaged() && spec.Runtime != nil && len(spec.Runtime.Command) > 0 {
		return fmt.Errorf("managed agent %q does not accept runtime command", name)
	}
	if !isAgentRuntimeType(runtimeType) {
		return fmt.Errorf("unsupported agent runtime %q", runtimeType)
	}
	if runtimeType != "" && reg.ProfileRequired(runtimeType) && agentProfile(spec) == "" {
		return fmt.Errorf("agent %q runtime %q requires an agent profile; pass --agent-profile or set the runtime profile in the native agent spec", name, runtimeType)
	}
	if reg.ValidateRuntime != nil {
		if spec.Name == "" {
			spec.Name = name
		}
		return reg.ValidateRuntime(spec)
	}
	return nil
}

func agentProfile(spec domain.AgentSpec) string {
	if profile := strings.TrimSpace(spec.Profile); profile != "" {
		return profile
	}
	if spec.Runtime == nil {
		return ""
	}
	return strings.TrimSpace(spec.Runtime.Profile)
}

type Selection struct {
	Name        string
	RuntimeType domain.AgentRuntimeType
	Image       string
	Profile     string
	BackendName string
}

// ValidateSelection validates agent/runtime/profile/backend compatibility
// using declarative registration metadata.
func (r *Registry) ValidateSelection(selection Selection) error {
	if err := r.validateRegistration(selection.Name, selection.RuntimeType); err != nil {
		return err
	}
	if !isAgentRuntimeType(selection.RuntimeType) {
		return fmt.Errorf("unsupported agent runtime %q", selection.RuntimeType)
	}
	if selection.RuntimeType == domain.AgentRuntimeTypeAgentImage && strings.TrimSpace(selection.Image) == "" {
		return fmt.Errorf("agent runtime agent-image requires agent bundle image")
	}
	reg := r.entries[selection.Name]
	if selection.RuntimeType != "" {
		if reg.ProfileRequired(selection.RuntimeType) && strings.TrimSpace(selection.Profile) == "" {
			return fmt.Errorf("agent %q runtime %q requires an agent profile; pass --agent-profile for profile-backed mounted bundle execution", selection.Name, selection.RuntimeType)
		}
	}
	return nil
}

func (r *Registry) validateRegistration(name string, runtimeType domain.AgentRuntimeType) error {
	reg, ok := r.entries[name]
	if !ok {
		return fmt.Errorf("unknown agent %q; known agents: %s", name, strings.Join(r.Names(), ", "))
	}
	if reg.SupportsRuntime(runtimeType) {
		return nil
	}
	return fmt.Errorf("agent %q does not support runtime %q; supported: %v", name, runtimeType, reg.SupportedRuntimes)
}

// HasCapability reports whether the named registration declares the
// given capability string.
func (reg Registration) HasCapability(capability string) bool {
	return slices.Contains(reg.RequiredCapabilities, capability)
}

// SupportsRuntime reports whether the registration declares support for the
// given runtime type. An empty runtime type is treated as unspecified and valid.
func (reg Registration) SupportsRuntime(runtimeType domain.AgentRuntimeType) bool {
	return runtimeType == "" || len(reg.SupportedRuntimes) == 0 || slices.Contains(reg.SupportedRuntimes, runtimeType)
}

// IsSelfContained reports whether the agent harness can run without an
// external command flag on the local backend.
func (reg Registration) IsSelfContained() bool {
	return reg.RunByDefault || reg.DefaultRuntimeType != ""
}

// ProfileRequired reports whether the given runtime must be selected with an
// explicit named profile for this agent registration.
func (reg Registration) ProfileRequired(runtimeType domain.AgentRuntimeType) bool {
	return slices.Contains(reg.ProfileRequiredRuntimes, runtimeType)
}

func isAgentRuntimeType(runtimeType domain.AgentRuntimeType) bool {
	switch runtimeType {
	case "", domain.AgentRuntimeTypeSandboxCommand, domain.AgentRuntimeTypeAgentImage, domain.AgentRuntimeTypeOracle:
		return true
	default:
		return false
	}
}
