package agentprofile

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
)

type CreateParams struct {
	Namespace      string
	Name           string
	Spec           *agentprofilev1.AgentProfileSpec
	Labels         map[string]string
	Credential     []byte
	IdempotencyKey string
}

type UpdateParams struct {
	Namespace       string
	Name            string
	Patch           *agentprofilev1.AgentProfilePatch
	ExpectedVersion int64
	IdempotencyKey  string
}

type RotateParams struct {
	Namespace       string
	Name            string
	Credential      []byte
	ExpectedVersion int64
	IdempotencyKey  string
}
type DoctorParams struct{ Namespace, Name, Model string }

type Snapshot struct {
	Profile            *agentprofilev1.AgentProfile
	CredentialSecretID string
	CredentialVersion  int64
}

type Control interface {
	Create(ctx context.Context, params CreateParams, now time.Time) (*agentprofilev1.AgentProfile, error)
	Get(ctx context.Context, namespace, name string) (*agentprofilev1.AgentProfile, bool, error)
	List(ctx context.Context, filter *agentprofilev1.AgentProfileListFilter) ([]*agentprofilev1.AgentProfile, string, error)
	Update(ctx context.Context, params UpdateParams, now time.Time) (*agentprofilev1.AgentProfile, error)
	Rotate(ctx context.Context, params RotateParams, now time.Time) (*agentprofilev1.AgentProfile, error)
	Delete(ctx context.Context, namespace, name string, expectedVersion int64) (*agentprofilev1.AgentProfile, bool, error)
	ResolveSnapshot(ctx context.Context, namespace, name string) (*Snapshot, bool, error)
	Doctor(ctx context.Context, params DoctorParams, now time.Time) (*agentprofilev1.DoctorAgentProfileResponse, error)
}

func ValidateCreate(params CreateParams) error {
	if strings.TrimSpace(params.Namespace) == "" {
		return fmt.Errorf("namespace is required")
	}
	if strings.TrimSpace(params.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if params.Spec == nil {
		return fmt.Errorf("spec is required")
	}
	if strings.TrimSpace(params.Spec.GetAgent()) == "" {
		return fmt.Errorf("agent is required")
	}
	if params.Spec.GetProvider() == agentprofilev1.AgentProvider_AGENT_PROVIDER_UNSPECIFIED {
		return fmt.Errorf("provider is required")
	}
	if params.Spec.GetWireApi() == agentprofilev1.AgentWireApi_AGENT_WIRE_API_UNSPECIFIED {
		return fmt.Errorf("wire_api is required")
	}
	if !compatible(params.Spec.GetAgent(), params.Spec.GetProvider(), params.Spec.GetWireApi()) {
		return fmt.Errorf("agent %q is incompatible with provider %s and wire_api %s", params.Spec.GetAgent(), params.Spec.GetProvider(), params.Spec.GetWireApi())
	}
	parsed, err := url.Parse(strings.TrimSpace(params.Spec.GetBaseUrl()))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("base_url must be an absolute HTTPS URL without user information, query, or fragment")
	}
	if err := ValidateCredential(params.Credential); err != nil {
		return err
	}
	if params.Spec.GetMaxConcurrency() <= 0 {
		return fmt.Errorf("max_concurrency must be greater than zero")
	}
	return nil
}

func ValidateCredential(credential []byte) error {
	if len(credential) == 0 {
		return fmt.Errorf("credential is required")
	}
	if len(credential) > 16*1024 {
		return fmt.Errorf("credential exceeds 16 KiB limit")
	}
	if strings.TrimSpace(string(credential)) == "" {
		return fmt.Errorf("credential must not be empty")
	}
	return nil
}

func compatible(agent string, provider agentprofilev1.AgentProvider, wire agentprofilev1.AgentWireApi) bool {
	switch strings.TrimSpace(agent) {
	case "codex":
		return provider == agentprofilev1.AgentProvider_AGENT_PROVIDER_OPENAI && wire == agentprofilev1.AgentWireApi_AGENT_WIRE_API_OPENAI_RESPONSES
	case "claude-code":
		return provider == agentprofilev1.AgentProvider_AGENT_PROVIDER_ANTHROPIC && wire == agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES
	default:
		return false
	}
}
