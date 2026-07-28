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
	Paths          Paths
	SandboxRuntime sandbox.Runtime
	AgentHarness   agent.Harness
	Now            func() time.Time
	RuntimeName    string
	HealthCheck    HealthCheckConfig
	PhaseReporter  domain.PhaseReporter
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

	sandboxPhaseStart := time.Now()
	session.emitPhase(domain.RolloutPhaseSandboxCreating, domain.PhaseStatusStarted, sandboxPhaseStart, nil)
	instance, err := session.createSandbox(ctx)
	if err != nil {
		session.emitPhase(domain.RolloutPhaseSandboxCreating, domain.PhaseStatusFailed, sandboxPhaseStart, err)
		failedEpisode, failErr := session.failEarlyInfrastructure(err, "sandbox_create")
		return failedEpisode, failErr
	}
	defer session.cleanupSandbox(ctx, instance, &runErr)

	if err := session.recordSandboxStart(instance); err != nil {
		session.emitPhase(domain.RolloutPhaseSandboxCreating, domain.PhaseStatusFailed, sandboxPhaseStart, err)
		return session.episode, err
	}
	session.emitPhase(domain.RolloutPhaseSandboxCreating, domain.PhaseStatusCompleted, sandboxPhaseStart, nil)
	episode = session.episode

	// Phase 3: Upload workspace and capture baseline.
	baseline, err := session.prepareWorkspace(ctx, instance)
	if err != nil {
		failedEpisode, failErr := session.failEarlyInfrastructure(err, "workspace_upload")
		return failedEpisode, failErr
	}
	episode = session.episode

	// Phase 4: Run agent phase and resolve terminal failures.
	agentPhaseStart := time.Now()
	session.emitPhase(domain.RolloutPhaseAgentRunning, domain.PhaseStatusStarted, agentPhaseStart, nil)
	shouldVerify, err := session.runAgentPhase(ctx, instance, baseline)
	episode = session.episode
	if err != nil {
		session.emitPhase(domain.RolloutPhaseAgentRunning, domain.PhaseStatusFailed, agentPhaseStart, err)
		return episode, err
	}
	session.emitPhase(domain.RolloutPhaseAgentRunning, domain.PhaseStatusCompleted, agentPhaseStart, nil)
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
	verifierPhaseStart := time.Now()
	session.emitPhase(domain.RolloutPhaseVerifying, domain.PhaseStatusStarted, verifierPhaseStart, nil)
	if err := session.runVerifierPhase(ctx, instance); err != nil {
		session.emitPhase(domain.RolloutPhaseVerifying, domain.PhaseStatusFailed, verifierPhaseStart, err)
		return session.episode, err
	}
	session.emitPhase(domain.RolloutPhaseVerifying, domain.PhaseStatusCompleted, verifierPhaseStart, nil)
	return session.episode, nil
}
