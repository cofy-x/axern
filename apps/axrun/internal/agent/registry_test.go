package agent

import (
	"errors"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

var errTestRuntimePolicy = errors.New("test runtime policy")

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Registration{Name: "test-agent", Provider: ProviderNone, Kind: RegistrationKindBuiltin}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	reg, ok := r.Lookup("test-agent")
	if !ok {
		t.Fatal("Lookup returned false for registered agent")
	}
	if reg.Name != "test-agent" || reg.Provider != ProviderNone {
		t.Fatalf("unexpected registration: %+v", reg)
	}
}

func TestRegistryLookupUnknown(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Lookup("nonexistent")
	if ok {
		t.Fatal("Lookup returned true for unregistered agent")
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(builtinRegistration("dup")); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}
	if err := r.Register(builtinRegistration("dup")); err == nil {
		t.Fatal("duplicate Register should fail")
	}
}

func TestRegistryRejectsEmptyName(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Registration{Name: ""}); err == nil {
		t.Fatal("empty name Register should fail")
	}
	if err := r.Register(Registration{Name: "   "}); err == nil {
		t.Fatal("whitespace name Register should fail")
	}
}

func TestRegistryRejectsUnknownKind(t *testing.T) {
	err := NewRegistry().Register(Registration{Name: "broken", Kind: RegistrationKind("other")})
	if err == nil {
		t.Fatal("unsupported registration kind was accepted")
	}
}

func TestRegistryRejectsMissingKind(t *testing.T) {
	err := NewRegistry().Register(Registration{Name: "ambiguous"})
	if err == nil {
		t.Fatal("registration without kind was accepted")
	}
}

func TestRegistryNames(t *testing.T) {
	r := NewRegistry()
	r.Register(builtinRegistration("bravo"))
	r.Register(builtinRegistration("alpha"))
	r.Register(builtinRegistration("charlie"))

	names := r.Names()
	if len(names) != 3 || names[0] != "alpha" || names[1] != "bravo" || names[2] != "charlie" {
		t.Fatalf("Names() = %v", names)
	}
}

func TestRegistryIsKnown(t *testing.T) {
	r := NewRegistry()
	r.Register(builtinRegistration("present"))
	if !r.IsKnown("present") {
		t.Fatal("IsKnown returned false for registered agent")
	}
	if r.IsKnown("absent") {
		t.Fatal("IsKnown returned true for unregistered agent")
	}
}

func TestRegistryNewHarnessWithFactory(t *testing.T) {
	r := NewRegistry()
	r.Register(Registration{
		Name: "noop",
		Kind: RegistrationKindBuiltin,
		HarnessFactory: func(spec domain.AgentSpec) (Harness, error) {
			return NoopHarness{}, nil
		},
	})
	h, err := r.NewHarness("noop", domain.AgentSpec{Name: "noop"})
	if err != nil {
		t.Fatalf("NewHarness failed: %v", err)
	}
	if h == nil {
		t.Fatal("NewHarness returned nil harness")
	}
}

func TestRegistryNewHarnessNilFactory(t *testing.T) {
	r := NewRegistry()
	r.Register(builtinRegistration("oracle"))
	h, err := r.NewHarness("oracle", domain.AgentSpec{Name: "oracle"})
	if err != nil {
		t.Fatalf("NewHarness failed: %v", err)
	}
	if h != nil {
		t.Fatal("NewHarness should return nil for nil factory")
	}
}

func TestRegistryNewHarnessUnknown(t *testing.T) {
	r := NewRegistry()
	_, err := r.NewHarness("unknown", domain.AgentSpec{Name: "unknown"})
	if err == nil {
		t.Fatal("NewHarness should fail for unknown agent")
	}
}

func TestRegistryValidateAgent(t *testing.T) {
	r := NewRegistry()
	r.Register(Registration{
		Name:              "test-agent",
		Kind:              RegistrationKindBuiltin,
		SupportedRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeSandboxCommand},
	})

	if err := r.ValidateAgent("test-agent", domain.AgentSpec{}); err != nil {
		t.Fatalf("empty runtime should pass: %v", err)
	}
	if err := r.ValidateAgent("test-agent", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeSandboxCommand},
	}); err != nil {
		t.Fatalf("supported runtime should pass: %v", err)
	}
	if err := r.ValidateAgent("test-agent", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeAgentImage},
	}); err == nil {
		t.Fatal("unsupported runtime should fail")
	}
	if err := r.ValidateAgent("unknown", domain.AgentSpec{}); err == nil {
		t.Fatal("unknown agent should fail")
	}
}

func TestRegistryValidateAgentFallsBackToSpecName(t *testing.T) {
	r := NewRegistry()
	r.Register(builtinRegistration("spec-agent"))

	if err := r.ValidateAgent("", domain.AgentSpec{Name: "spec-agent"}); err != nil {
		t.Fatalf("should fall back to spec name: %v", err)
	}
	if err := r.ValidateAgent("", domain.AgentSpec{Name: "nonexistent"}); err == nil {
		t.Fatal("unknown fallback name should fail")
	}
}

func TestRegistryValidateAgentFlexibleRegistration(t *testing.T) {
	r := NewRegistry()
	r.Register(builtinRegistration("flexible"))

	if err := r.ValidateAgent("flexible", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeSandboxCommand},
	}); err != nil {
		t.Fatalf("agent with no declared runtimes should accept known runtimes: %v", err)
	}
	if err := r.ValidateAgent("flexible", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: "sidecar"},
	}); err == nil {
		t.Fatal("unknown runtime should fail even for flexible registrations")
	}
}

func TestRegistryValidateAgentRequiresProfileForPersistedSpec(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Registration{
		Name:                    "profile-agent",
		Kind:                    RegistrationKindBuiltin,
		SupportedRuntimes:       []domain.AgentRuntimeType{domain.AgentRuntimeTypeAgentImage},
		ProfileRequiredRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeAgentImage},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	err := r.ValidateAgent("profile-agent", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeAgentImage},
	})
	if err == nil {
		t.Fatal("agent-image runtime without profile should fail")
	}
	if err := r.ValidateAgent("profile-agent", domain.AgentSpec{
		Profile: "deepseek",
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeAgentImage},
	}); err != nil {
		t.Fatalf("top-level profile should pass: %v", err)
	}
	if err := r.ValidateAgent("profile-agent", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeAgentImage, Profile: "deepseek"},
	}); err != nil {
		t.Fatalf("runtime profile should pass: %v", err)
	}
}

func TestRegistrationHasCapability(t *testing.T) {
	reg := Registration{RequiredCapabilities: []string{"shell", "patch"}}
	if !reg.HasCapability("shell") {
		t.Fatal("HasCapability(shell) = false")
	}
	if reg.HasCapability("tunnel") {
		t.Fatal("HasCapability(tunnel) = true")
	}
}

func TestRegistrationSupportsRuntime(t *testing.T) {
	reg := Registration{SupportedRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeSandboxCommand}}
	if !reg.SupportsRuntime("") {
		t.Fatal("SupportsRuntime(empty) = false")
	}
	if !reg.SupportsRuntime(domain.AgentRuntimeTypeSandboxCommand) {
		t.Fatal("SupportsRuntime(sandbox-command) = false")
	}
	if reg.SupportsRuntime(domain.AgentRuntimeTypeAgentImage) {
		t.Fatal("SupportsRuntime(agent-image) = true")
	}

	flexible := Registration{}
	if !flexible.SupportsRuntime(domain.AgentRuntimeTypeAgentImage) {
		t.Fatal("flexible SupportsRuntime(agent-image) = false")
	}
}

func TestRegistryValidateSelection(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Registration{
		Name:                    "claude-code",
		Kind:                    RegistrationKindManaged,
		SupportedRuntimes:       []domain.AgentRuntimeType{domain.AgentRuntimeTypeSandboxCommand, domain.AgentRuntimeTypeAgentImage},
		ProfileRequiredRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeAgentImage},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if err := r.ValidateSelection(Selection{
		Name:        "claude-code",
		RuntimeType: domain.AgentRuntimeTypeSandboxCommand,
		BackendName: "local",
	}); err != nil {
		t.Fatalf("ValidateSelection local sandbox-command failed: %v", err)
	}
	if err := r.ValidateSelection(Selection{
		Name:        "claude-code",
		RuntimeType: domain.AgentRuntimeTypeAgentImage,
		BackendName: "axern",
		Image:       "axern/claude-code-bundle:dev",
		Profile:     "deepseek",
	}); err != nil {
		t.Fatalf("ValidateSelection agent-image failed: %v", err)
	}
	if err := r.ValidateSelection(Selection{
		Name:        "claude-code",
		RuntimeType: domain.AgentRuntimeTypeAgentImage,
		BackendName: "axern",
		Profile:     "deepseek",
	}); err == nil {
		t.Fatal("ValidateSelection should fail when agent bundle image is missing")
	}
	if err := r.ValidateSelection(Selection{
		Name:        "claude-code",
		RuntimeType: "sidecar",
		BackendName: "axern",
	}); err == nil {
		t.Fatal("ValidateSelection should fail for unknown runtime")
	}
	if err := r.ValidateSelection(Selection{
		Name:        "claude-code",
		RuntimeType: domain.AgentRuntimeTypeAgentImage,
		BackendName: "axern",
		Image:       "axern/claude-code-bundle:dev",
	}); err == nil {
		t.Fatal("ValidateSelection should fail when profile is missing")
	}
}

func TestRegistryValidateAgentRuntimePolicy(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Registration{
		Name:              "policy-agent",
		Kind:              RegistrationKindBuiltin,
		SupportedRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeAgentImage},
		ValidateRuntime: func(spec domain.AgentSpec) error {
			if spec.Runtime == nil || spec.Runtime.Image == "" {
				t.Fatal("runtime spec was not passed to validator")
			}
			return errTestRuntimePolicy
		},
	}); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	err := r.ValidateAgent("policy-agent", domain.AgentSpec{
		Runtime: &domain.AgentRuntimeSpec{
			Type:  domain.AgentRuntimeTypeAgentImage,
			Image: "axern/policy-agent:dev",
		},
	})
	if err != errTestRuntimePolicy {
		t.Fatalf("ValidateAgent error = %v, want policy error", err)
	}
}

func builtinRegistration(name string) Registration {
	return Registration{Name: name, Kind: RegistrationKindBuiltin}
}
