package managedworker

import (
	"context"
	"fmt"
	"net/url"

	"github.com/cofy-x/axern/lib/go/agentprofile"
	agentprofilev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/agentprofile/v1"
	workerrolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/worker/v1"
)

type resolvedAgentProfile struct {
	Snapshot *workerrolloutv1.ResolvedAgentProfile
	Runtime  agentprofile.Profile
}

func (w Worker) resolveAgentProfile(ctx context.Context, work *workerrolloutv1.WorkItem, leaseToken string) (resolvedAgentProfile, error) {
	response, err := w.client.ResolveAgentProfile(ctx, &workerrolloutv1.ResolveAgentProfileRequest{
		WorkID:     work.GetID(),
		LeaseToken: leaseToken,
	})
	if err != nil {
		return resolvedAgentProfile{}, err
	}
	resolved := response.GetProfile()
	profile, err := profileFromControl(resolved)
	if err != nil {
		return resolvedAgentProfile{}, err
	}
	return resolvedAgentProfile{Snapshot: resolved, Runtime: profile}, nil
}

func profileFromControl(resolved *workerrolloutv1.ResolvedAgentProfile) (agentprofile.Profile, error) {
	profile := resolved.GetProfile()
	if profile == nil {
		return agentprofile.Profile{}, fmt.Errorf("resolved profile is missing")
	}
	upstream, err := url.Parse(profile.GetSpec().GetBaseUrl())
	if err != nil {
		return agentprofile.Profile{}, err
	}
	provider := agentprofile.ProviderOpenAI
	if profile.GetSpec().GetProvider() == agentprofilev1.AgentProvider_AGENT_PROVIDER_ANTHROPIC {
		provider = agentprofile.ProviderAnthropic
	}
	wire := agentprofile.WireAPIResponses
	if profile.GetSpec().GetWireApi() == agentprofilev1.AgentWireApi_AGENT_WIRE_API_ANTHROPIC_MESSAGES {
		wire = agentprofile.WireAPIAnthropicMessages
	}
	return agentprofile.Profile{
		Name:         profile.GetID(),
		Agent:        agentprofile.AgentType(profile.GetSpec().GetAgent()),
		ProviderType: provider,
		WireAPI:      wire,
		Upstream:     upstream,
		Token:        resolved.GetToken(),
	}, nil
}
