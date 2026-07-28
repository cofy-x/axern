package rollout

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func providerForType(pt agent.ProviderType) proxy.Provider {
	switch pt {
	case agent.ProviderAnthropic:
		return proxy.AnthropicProvider()
	case agent.ProviderOpenAI:
		return proxy.OpenAIProvider()
	default:
		return nil
	}
}

func managedProxyProviderName(pt agent.ProviderType) (string, error) {
	switch pt {
	case agent.ProviderAnthropic:
		return "anthropic", nil
	case agent.ProviderOpenAI:
		return "openai", nil
	default:
		return "", fmt.Errorf("no managed proxy provider for agent provider type %q", pt)
	}
}

func setupManagedProxy(config agent.ManagedProxyConfig, recorder *proxy.Recorder) (*sandbox.ManagedProxyOptions, error) {
	provider := providerForType(config.ProviderType)
	if provider == nil {
		return nil, fmt.Errorf("no recorder provider for agent provider type %q", config.ProviderType)
	}
	if recorder != nil {
		recorder.SetProvider(provider)
	}
	providerName, err := managedProxyProviderName(config.ProviderType)
	if err != nil {
		return nil, err
	}
	if config.Upstream == nil {
		return nil, fmt.Errorf("managed proxy upstream is required; set upstream in the selected agent profile")
	}
	if config.Token == "" {
		return nil, fmt.Errorf("managed proxy token is required; set token in the selected agent profile")
	}
	return &sandbox.ManagedProxyOptions{
		Provider:            providerName,
		UpstreamBaseURL:     config.Upstream.String(),
		UpstreamBearerToken: config.Token,
	}, nil
}
