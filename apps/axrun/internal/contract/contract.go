package contract

import (
	"fmt"
	"path"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const MaxPathSegmentBytes = 96

func ValidatePathSegment(name string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if strings.ContainsAny(value, `/\`) || value == "." || value == ".." {
		return fmt.Errorf("%s %q must be a single path segment", name, value)
	}
	if len(value) > MaxPathSegmentBytes {
		return fmt.Errorf("%s must not exceed %d UTF-8 bytes", name, MaxPathSegmentBytes)
	}
	return nil
}

func ValidateVerifierSpec(taskID string, verifier domain.VerifierSpec) error {
	if verifier.TimeoutSec < 0 {
		return fmt.Errorf("task %q verifier timeout_sec must be non-negative", taskID)
	}
	for index, asset := range verifier.Assets {
		if strings.TrimSpace(asset.Path) == "" {
			return fmt.Errorf("task %q verifier assets[%d].path is required", taskID, index)
		}
		targetPath := strings.TrimSpace(asset.TargetPath)
		if targetPath != "" && !strings.HasPrefix(targetPath, "/") {
			return fmt.Errorf("task %q verifier assets[%d].target_path must be absolute", taskID, index)
		}
		if path.Clean(targetPath) == "/" {
			return fmt.Errorf("task %q verifier assets[%d].target_path must not be the sandbox root", taskID, index)
		}
	}
	switch verifier.Type {
	case domain.VerifierTypeNone:
		if verifier.Command != "" || verifier.CWD != "" || verifier.TimeoutSec != 0 || len(verifier.Assets) != 0 {
			return fmt.Errorf("task %q none verifier cannot include shell fields", taskID)
		}
	case domain.VerifierTypeShell:
		if verifier.Command == "" {
			return fmt.Errorf("task %q shell verifier command is required", taskID)
		}
	default:
		return fmt.Errorf("task %q verifier type %q is not supported", taskID, verifier.Type)
	}
	return nil
}

func ValidateTimeoutPolicy(taskID string, timeouts *domain.TimeoutPolicy) error {
	if timeouts == nil {
		return nil
	}
	if timeouts.AgentSec < 0 {
		return fmt.Errorf("task %q agent timeout must be non-negative", taskID)
	}
	if timeouts.VerifierSec < 0 {
		return fmt.Errorf("task %q verifier timeout must be non-negative", taskID)
	}
	if timeouts.EpisodeSec < 0 {
		return fmt.Errorf("task %q episode timeout must be non-negative", taskID)
	}
	return nil
}

func IsRunStatus(value domain.RunStatus) bool {
	switch value {
	case domain.RunStatusCreated, domain.RunStatusRunning, domain.RunStatusCompleted, domain.RunStatusFailed:
		return true
	default:
		return false
	}
}

func IsEpisodeStatus(value domain.EpisodeStatus) bool {
	switch value {
	case domain.EpisodeStatusPending, domain.EpisodeStatusRunning, domain.EpisodeStatusVerifying, domain.EpisodeStatusCompleted, domain.EpisodeStatusFailed:
		return true
	default:
		return false
	}
}

func IsAgentStatus(value domain.AgentStatus) bool {
	switch value {
	case domain.AgentStatusPending, domain.AgentStatusRunning, domain.AgentStatusCompleted, domain.AgentStatusFailed, domain.AgentStatusSkipped:
		return true
	default:
		return false
	}
}

func IsAgentExitReason(value domain.AgentExitReason) bool {
	switch value {
	case domain.AgentExitReasonCompleted,
		domain.AgentExitReasonCommandNonzero,
		domain.AgentExitReasonIdleTimeout,
		domain.AgentExitReasonProxyNoRequests,
		domain.AgentExitReasonLLMError,
		domain.AgentExitReasonCompletedNoPatch,
		domain.AgentExitReasonInfrastructure,
		domain.AgentExitReasonTimeout:
		return true
	default:
		return false
	}
}

func IsFinalAgentStatus(value domain.AgentStatus) bool {
	switch value {
	case domain.AgentStatusCompleted, domain.AgentStatusFailed, domain.AgentStatusSkipped:
		return true
	default:
		return false
	}
}

func IsRewardStatus(value domain.RewardStatus) bool {
	switch value {
	case domain.RewardStatusPending, domain.RewardStatusScored, domain.RewardStatusUnscored, domain.RewardStatusAgentFailed, domain.RewardStatusInvalid, domain.RewardStatusInfraFailed:
		return true
	default:
		return false
	}
}

func IsFailureClass(value domain.FailureClass) bool {
	switch value {
	case domain.FailureClassAgentFailed,
		domain.FailureClassVerifierFailed,
		domain.FailureClassInfrastructure,
		domain.FailureClassPatchEmpty,
		domain.FailureClassPatchInvalid,
		domain.FailureClassTimeout:
		return true
	default:
		return false
	}
}

func IsVerifierType(value domain.VerifierType) bool {
	switch value {
	case domain.VerifierTypeNone, domain.VerifierTypeShell:
		return true
	default:
		return false
	}
}

func IsAgentRuntimeType(value domain.AgentRuntimeType) bool {
	switch value {
	case domain.AgentRuntimeTypeAgentImage, domain.AgentRuntimeTypeSandboxCommand, domain.AgentRuntimeTypeOracle:
		return true
	default:
		return false
	}
}

func IsSandboxRuntimeSourceType(value domain.SandboxRuntimeSourceType) bool {
	switch value {
	case domain.SandboxRuntimeSourceTemplate, domain.SandboxRuntimeSourceImage, domain.SandboxRuntimeSourceDockerfile:
		return true
	default:
		return false
	}
}

func IsArtifactKind(value domain.ArtifactKind) bool {
	switch value {
	case domain.ArtifactKindAgentRawLog,
		domain.ArtifactKindAgentStdout,
		domain.ArtifactKindAgentStderr,
		domain.ArtifactKindDownloadedFile,
		domain.ArtifactKindDownloadedDir,
		domain.ArtifactKindLLMTelemetry,
		domain.ArtifactKindPatch,
		domain.ArtifactKindRuntimeImageBuild,
		domain.ArtifactKindTrajectoryExport,
		domain.ArtifactKindTrainingDataExport,
		domain.ArtifactKindVerifierBreakdown:
		return true
	default:
		return false
	}
}

func IsArtifactRole(value domain.ArtifactRole) bool {
	switch value {
	case domain.ArtifactRoleInput, domain.ArtifactRoleOutput, domain.ArtifactRoleRaw, domain.ArtifactRoleDerived, domain.ArtifactRoleExport:
		return true
	default:
		return false
	}
}

func IsRolloutPhase(value domain.RolloutPhase) bool {
	switch value {
	case domain.RolloutPhasePlanning,
		domain.RolloutPhasePreparingInputs,
		domain.RolloutPhaseSandboxCreating,
		domain.RolloutPhaseAgentRunning,
		domain.RolloutPhaseVerifying,
		domain.RolloutPhaseCollecting,
		domain.RolloutPhaseValidating,
		domain.RolloutPhaseExporting:
		return true
	default:
		return false
	}
}

func IsPhaseStatus(value domain.PhaseStatus) bool {
	switch value {
	case domain.PhaseStatusStarted, domain.PhaseStatusCompleted, domain.PhaseStatusFailed:
		return true
	default:
		return false
	}
}

func IsRolloutErrorCode(value domain.RolloutErrorCode) bool {
	switch value {
	case domain.RolloutErrorInputInvalid,
		domain.RolloutErrorInputResolutionFailed,
		domain.RolloutErrorTaskRuntimeSourceMissing,
		domain.RolloutErrorRuntimeImagePrepareFailed,
		domain.RolloutErrorSandboxCreateFailed,
		domain.RolloutErrorAgentFailed,
		domain.RolloutErrorAgentTimeout,
		domain.RolloutErrorVerifierFailed,
		domain.RolloutErrorVerifierTimeout,
		domain.RolloutErrorArtifactCaptureFailed,
		domain.RolloutErrorValidationFailed,
		domain.RolloutErrorExportNotReady,
		domain.RolloutErrorInfrastructureFailure:
		return true
	default:
		return false
	}
}

func IsArtifactManifestStatus(value domain.ArtifactManifestStatus) bool {
	switch value {
	case domain.ArtifactManifestStatusPresent, domain.ArtifactManifestStatusMissing, domain.ArtifactManifestStatusFailed:
		return true
	default:
		return false
	}
}

// IsEpisodeComplete returns true when an episode has reached a terminal
// state with all expected output records present: terminal status,
// completed_at timestamp (the atomic commit marker), agent result path,
// reward path, and the reward marked as final.
func IsEpisodeComplete(episode domain.Episode, reward domain.Reward) bool {
	if episode.Status != domain.EpisodeStatusCompleted && episode.Status != domain.EpisodeStatusFailed {
		return false
	}
	if episode.FinishedAt == nil {
		return false
	}
	if episode.CompletedAt == nil {
		return false
	}
	if episode.AgentResultPath == "" {
		return false
	}
	if episode.RewardPath == "" {
		return false
	}
	return reward.Final
}

// IsEpisodeExportReady validates minimal export gating conditions on top of
// IsEpisodeComplete. It ensures key agent artifact refs are structurally safe
// and that LLM telemetry counts are not exported without a raw log reference.
func IsEpisodeExportReady(episode domain.Episode, reward domain.Reward, agent domain.AgentResult) bool {
	if !IsEpisodeComplete(episode, reward) {
		return false
	}
	if agent.RawLogRef != "" && !isRunRelativeRef(agent.RawLogRef) {
		return false
	}
	if agent.PatchRef != "" && !isRunRelativeRef(agent.PatchRef) {
		return false
	}
	if agent.RawLogRef != "" && !hasArtifactRef(agent.Artifacts, agent.RawLogRef) {
		return false
	}
	if (agent.LLMRequestCount > 0 || agent.LLMResponseCount > 0 || agent.LLMErrorCount > 0) && agent.RawLogRef == "" {
		return false
	}
	return true
}

func hasArtifactRef(artifacts []domain.ArtifactRef, ref string) bool {
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) == strings.TrimSpace(ref) {
			return true
		}
	}
	return false
}

func isRunRelativeRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return false
	}
	if strings.HasPrefix(ref, "/") {
		return false
	}
	clean := path.Clean(ref)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func IsAgentRawEventType(value domain.AgentRawEventType) bool {
	switch value {
	case domain.AgentRawEventCommandStarted,
		domain.AgentRawEventCommandFinished,
		domain.AgentRawEventToolCall,
		domain.AgentRawEventToolResult,
		domain.AgentRawEventPatch,
		domain.AgentRawEventArtifact,
		domain.AgentRawEventLLMRequest,
		domain.AgentRawEventLLMResponse,
		domain.AgentRawEventLLMChunk,
		domain.AgentRawEventLLMDone,
		domain.AgentRawEventLLMError,
		domain.AgentRawEventLLMTruncated:
		return true
	default:
		return false
	}
}

func IsTrajectoryEventType(value domain.TrajectoryEventType) bool {
	switch value {
	case domain.TrajectoryEventSystemResumeStarted,
		domain.TrajectoryEventSystemSandboxStarting,
		domain.TrajectoryEventSystemSandboxStarted,
		domain.TrajectoryEventSystemWorkspaceUpload,
		domain.TrajectoryEventSystemWorkspaceBaseline,
		domain.TrajectoryEventSystemImageBuildStart,
		domain.TrajectoryEventSystemImageBuildDone,
		domain.TrajectoryEventSystemCleanupComplete,
		domain.TrajectoryEventSystemCleanupFailed,
		domain.TrajectoryEventSystemHealthCheckFailed,
		domain.TrajectoryEventSystemSandboxDeath,
		domain.TrajectoryEventSystemInfraFailure,
		domain.TrajectoryEventSystemTimeout,
		domain.TrajectoryEventAgentPlanned,
		domain.TrajectoryEventAgentStarted,
		domain.TrajectoryEventAgentStdout,
		domain.TrajectoryEventAgentStderr,
		domain.TrajectoryEventAgentFinished,
		domain.TrajectoryEventAgentToolCall,
		domain.TrajectoryEventAgentToolResult,
		domain.TrajectoryEventAgentLLMRequest,
		domain.TrajectoryEventAgentLLMResponse,
		domain.TrajectoryEventAgentLLMError,
		domain.TrajectoryEventPatchCreated,
		domain.TrajectoryEventArtifactCreated,
		domain.TrajectoryEventArtifactDownload,
		domain.TrajectoryEventVerifierPlanned,
		domain.TrajectoryEventVerifierFinished:
		return true
	default:
		return false
	}
}
