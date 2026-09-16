package rollout

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type Paths struct {
	EpisodeJSONPath  string
	TrajectoryPath   string
	AgentJSONPath    string
	VerifierJSONPath string
	RewardJSONPath   string
	ArtifactDir      string
}

type Store interface {
	WriteEpisode(path string, episode domain.Episode) error
	WriteAgentResult(path string, result domain.AgentResult) error
	WriteVerifierResult(path string, result domain.VerifierResult) error
	WriteReward(path string, reward domain.Reward) error
	WriteAgentArtifact(artifactDir string, filename string, content string) (string, error)
	AppendAgentRawEvent(artifactDir string, event domain.AgentRawEvent) (string, error)
	WriteArtifactManifest(artifactDir string, manifest domain.ArtifactManifest) (string, error)
	AppendTrajectoryStep(path string, step domain.TrajectoryStep) error
	CountTrajectorySteps(path string) (int, error)
}

// HealthCheckConfig configures sandbox health monitoring during episode
// execution. When Enabled is true, a background health probe runs during
// the agent phase and aborts the episode if the sandbox becomes unreachable.
type HealthCheckConfig struct {
	Enabled      bool
	Interval     time.Duration
	Threshold    int
	ProbeTimeout time.Duration
}

type Request struct {
	Store          Store
	Task           domain.TaskInstance
	Episode        domain.Episode
	Agent          domain.AgentSpec
	Model          domain.ModelSpec
	Artifacts      []domain.ArtifactRef
	Paths          Paths
	SandboxRuntime sandbox.Runtime
	AgentHarness   agent.Harness
	Now            func() time.Time
	RuntimeName    string
	HealthCheck    HealthCheckConfig
}

func Execute(request Request) (episode domain.Episode, runErr error) {
	// Phase 1: Preflight and execution wiring.
	session, err := prepareExecution(request)
	if err != nil {
		return request.Episode, err
	}
	episode = session.episode

	// Phase 2: Mark episode running and open sandbox lifecycle.
	if err := session.startEpisode(); err != nil {
		return session.episode, err
	}
	episode = session.episode
	ctx, cancel := session.createEpisodeContext()
	defer cancel()

	instance, err := session.createSandbox(ctx)
	if err != nil {
		failedEpisode, failErr := session.failEarlyInfrastructure(err, "sandbox_create")
		return failedEpisode, failErr
	}
	defer session.cleanupSandbox(ctx, instance, &runErr)

	if err := session.recordSandboxStart(instance); err != nil {
		return session.episode, err
	}
	episode = session.episode

	// Phase 3: Upload workspace and capture baseline.
	baseline, err := session.prepareWorkspace(ctx, instance)
	if err != nil {
		failedEpisode, failErr := session.failEarlyInfrastructure(err, "workspace_upload")
		return failedEpisode, failErr
	}
	episode = session.episode

	// Phase 4: Run agent phase and resolve terminal failures.
	shouldVerify, err := session.runAgentPhase(ctx, instance, baseline)
	episode = session.episode
	if err != nil {
		return episode, err
	}
	if !shouldVerify {
		// Phase 5a: Finalize episode when verifier is skipped.
		if session.episode.CompletedAt != nil {
			return session.episode, nil
		}
		if err := session.finalizeWithoutVerifier(); err != nil {
			return session.episode, err
		}
		return session.episode, nil
	}
	// Phase 5b: Run verifier and finalize completed episode.
	if err := session.runVerifierPhase(ctx, instance); err != nil {
		return session.episode, err
	}
	return session.episode, nil
}
