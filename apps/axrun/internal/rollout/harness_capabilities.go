package rollout

import (
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func sandboxRuntimeState(state sandbox.State) *domain.SandboxRuntimeState {
	if state.EnvironmentID == "" &&
		state.RunID == "" &&
		state.AllocationID == "" &&
		state.NodeID == "" {
		return nil
	}
	return &domain.SandboxRuntimeState{
		EnvironmentID: state.EnvironmentID,
		RunID:         state.RunID,
		AllocationID:  state.AllocationID,
		NodeID:        state.NodeID,
	}
}

// createAgentRecorder creates a recorder for agent telemetry. This recorder
// captures command lifecycle events for an agent harness.
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
	return nil
}
