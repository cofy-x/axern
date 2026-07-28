package rollout

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func sandboxRuntimeState(state sandbox.State) *domain.SandboxRuntimeState {
	if state.EnvironmentID == "" &&
		state.ServiceID == "" &&
		state.AllocationID == "" &&
		state.NodeID == "" &&
		state.RuntimeClass == "" {
		return nil
	}
	return &domain.SandboxRuntimeState{
		EnvironmentID:         state.EnvironmentID,
		ServiceID:             state.ServiceID,
		AllocationID:          state.AllocationID,
		NodeID:                state.NodeID,
		RuntimeClass:          state.RuntimeClass,
		PayloadFormat:         state.PayloadFormat,
		PayloadDigest:         state.PayloadDigest,
		CacheHit:              state.CacheHit,
		ImageResolveMs:        state.ImageResolveMs,
		ImagePullMs:           state.ImagePullMs,
		CowPrepareMs:          state.CowPrepareMs,
		VerifierMaterializeMs: state.VerifierMaterializeMs,
	}
}

func resolveManagedProxy(pc agent.ManagedProxyConfigurer, agentSpec domain.AgentSpec, recorder *proxy.Recorder) (*sandbox.ManagedProxyOptions, error) {
	config, err := pc.ManagedProxyConfig(agentSpec)
	if err != nil {
		return nil, fmt.Errorf("get agent managed proxy config: %w", err)
	}
	if config == nil {
		return nil, nil
	}
	result, err := setupManagedProxy(*config, recorder)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// createAgentRecorder creates a recorder for agent telemetry. This recorder
// captures command lifecycle events and, when an LLM proxy is active, also
// captures LLM request/response data. A recorder is always created when an
// agent harness is configured.
func createAgentRecorder(request Request) (*proxy.Recorder, error) {
	if request.AgentHarness == nil || request.Paths.ArtifactDir == "" {
		return nil, nil
	}
	return proxy.NewRecorder(request.Paths.ArtifactDir, nil)
}

func resolveWorkdir(task domain.TaskInstance) string {
	if task.InitialState != nil && task.InitialState.Workdir != "" {
		return task.InitialState.Workdir
	}
	if task.Sandbox.Workdir != "" {
		return task.Sandbox.Workdir
	}
	return "/workspace"
}

// preflightCapabilities checks that the agent's requirements are
// satisfiable by the current runtime before creating a sandbox.
func preflightCapabilities(request Request) error {
	if pc, ok := request.AgentHarness.(agent.ManagedProxyConfigurer); ok {
		spec := request.Episode.Agent
		config, err := pc.ManagedProxyConfig(spec)
		if err != nil {
			return fmt.Errorf("check agent managed proxy requirements: %w", err)
		}
		if config != nil && request.RuntimeName == "local" {
			return fmt.Errorf("agent %q requires managed proxy telemetry (profile %q), but the local runtime does not support sandboxd managed proxy; use --runner axern", spec.Name, spec.Profile)
		}
	}
	return nil
}
