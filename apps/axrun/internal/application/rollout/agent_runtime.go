package rollout

import (
	"fmt"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/google/go-containerregistry/pkg/name"
)

func validateAgentSelection(registry *agent.Registry, selection agent.Selection) error {
	if selection.RuntimeType == domain.AgentRuntimeTypeAgentImage {
		digest, err := name.NewDigest(strings.TrimSpace(selection.Image), name.WeakValidation)
		if err != nil || !strings.Contains(digest.DigestStr(), "sha256:") || len(strings.TrimPrefix(digest.DigestStr(), "sha256:")) != 64 {
			return fmt.Errorf("agent image must use an immutable sha256 digest reference")
		}
	}
	if registry == nil {
		registry = agentcatalog.DefaultRegistry()
	}
	if err := registry.ValidateSelection(selection); err != nil {
		return err
	}
	if selection.BackendName != "" {
		return backend.ValidateAgentRuntimeSupport(selection.BackendName, domain.AgentSpec{
			Name: selection.Name,
			Runtime: &domain.AgentRuntimeSpec{
				Type:    selection.RuntimeType,
				Image:   selection.Image,
				Profile: selection.Profile,
			},
		})
	}
	return nil
}

func (s Service) validateRunAgentForBackend(run domain.RolloutRun, backendName string) error {
	runtimeType := domain.AgentRuntimeType("")
	image := ""
	profile := run.Agent.Profile
	if run.Agent.Runtime != nil {
		runtimeType = run.Agent.Runtime.Type
		image = run.Agent.Runtime.Image
		if run.Agent.Runtime.Profile != "" {
			profile = run.Agent.Runtime.Profile
		}
	}
	registry := s.registry()
	if err := validateAgentSelection(registry, agent.Selection{
		Name:        run.Agent.Name,
		RuntimeType: runtimeType,
		Image:       image,
		Profile:     profile,
		BackendName: backendName,
	}); err != nil {
		return err
	}
	registration, ok := registry.Lookup(run.Agent.Name)
	if ok && registration.IsManaged() {
		return validateApprovalPolicy(string(run.Agent.ApprovalPolicy), backendName)
	}
	return nil
}
