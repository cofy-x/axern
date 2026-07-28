package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

// AgentPreflight lets a backend declare whether it can execute the agent
// runtime encoded in the native run record.
type AgentPreflight interface {
	PreflightAgent(agent domain.AgentSpec) error
}

type ProviderPreflight interface {
	PreflightProvider(context.Context, domain.AgentSpec, domain.ModelSpec) error
}

type ProviderProfilePreflight interface {
	PreflightProviderProfile(domain.AgentSpec) error
}

func PreflightHarnessProfile(harness agent.Harness, agentSpec domain.AgentSpec) error {
	if harness == nil {
		return nil
	}
	configurer, ok := harness.(agent.ManagedProxyConfigurer)
	if !ok {
		return nil
	}
	_, err := configurer.ManagedProxyConfig(agentSpec)
	return err
}

func PreflightHarnessProvider(ctx context.Context, harness agent.Harness, agentSpec domain.AgentSpec, model domain.ModelSpec) error {
	if harness == nil {
		return nil
	}
	prober, ok := harness.(agent.ProviderCapabilityProber)
	if !ok {
		return nil
	}
	_, err := prober.ProbeProvider(ctx, agentSpec, model)
	return err
}

func ValidateAgentRuntimeSupport(backendName string, agent domain.AgentSpec) error {
	runtime := agent.Runtime
	switch Name(backendName) {
	case NameLocal:
		if agent.ApprovalPolicy == domain.AgentApprovalPolicyNever {
			return fmt.Errorf("local backend does not allow managed agent approval policy never")
		}
		if runtime == nil {
			return nil
		}
		if runtime.Type == domain.AgentRuntimeTypeAgentImage {
			return fmt.Errorf("agent runtime agent-image is not supported by the local backend; use --runner axern")
		}
		return nil
	case NameAxern:
		if agent.ApprovalPolicy == domain.AgentApprovalPolicyOnRequest {
			return fmt.Errorf("Axern backend requires managed agent approval policy never")
		}
		if runtime == nil {
			return nil
		}
		if runtime.Type == domain.AgentRuntimeTypeAgentImage && strings.TrimSpace(runtime.Image) == "" {
			return fmt.Errorf("agent runtime agent-image requires agent bundle image")
		}
		return nil
	default:
		return ValidateName(backendName)
	}
}
