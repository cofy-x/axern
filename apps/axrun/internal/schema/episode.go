package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func validateEpisodes(problems *collector, runDir string, run domain.RolloutRun) []domain.Episode {
	episodesDir := filepath.Join(runDir, "episodes")
	entries, err := os.ReadDir(episodesDir)
	if err != nil {
		problems.add(displayPath(runDir, episodesDir), "", fmt.Sprintf("read episodes directory: %v", err))
		return nil
	}
	var episodes []domain.Episode
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		episodeDir := filepath.Join(episodesDir, entry.Name())
		episodePath := filepath.Join(episodeDir, "episode.json")
		episode, ok := readJSON[domain.Episode](problems, runDir, episodePath)
		if !ok {
			continue
		}
		episodes = append(episodes, episode)
		validateEpisodeRecord(problems, runDir, episodePath, episode, run)
		validateEpisodeFiles(problems, runDir, episodeDir, episode)
	}
	return episodes
}

func validateEpisodeTaskRefs(problems *collector, runDir string, tasks taskIndex, episodes []domain.Episode) {
	for _, episode := range episodes {
		if episode.TaskID == "" {
			continue
		}
		if _, ok := tasks[episode.TaskID]; ok {
			continue
		}
		episodePath := filepath.Join(runDir, "episodes", episode.ID, "episode.json")
		problems.add(displayPath(runDir, episodePath), "task_id", fmt.Sprintf("referenced task %q is missing", episode.TaskID))
	}
}

func validateEpisodeAttemptCoverage(problems *collector, runDir string, run domain.RolloutRun, episodes []domain.Episode) {
	if run.AttemptsPerTask <= 0 {
		return
	}
	seen := map[string]map[int]string{}
	for _, episode := range episodes {
		if episode.TaskID == "" || episode.AttemptIndex <= 0 {
			continue
		}
		if run.ID != "" {
			expectedID := domain.NewEpisodeID(run.ID, episode.TaskID, episode.AttemptIndex)
			if episode.ID != "" && episode.ID != expectedID {
				episodePath := filepath.Join(runDir, "episodes", episode.ID, "episode.json")
				problems.add(displayPath(runDir, episodePath), "id", fmt.Sprintf("got %q, want %q", episode.ID, expectedID))
			}
		}
		if episode.AttemptIndex > run.AttemptsPerTask {
			episodePath := filepath.Join(runDir, "episodes", episode.ID, "episode.json")
			problems.add(displayPath(runDir, episodePath), "attempt_index", fmt.Sprintf("got %d, want <= attempts_per_task %d", episode.AttemptIndex, run.AttemptsPerTask))
		}
		if seen[episode.TaskID] == nil {
			seen[episode.TaskID] = map[int]string{}
		}
		if existing, ok := seen[episode.TaskID][episode.AttemptIndex]; ok {
			episodePath := filepath.Join(runDir, "episodes", episode.ID, "episode.json")
			problems.add(displayPath(runDir, episodePath), "attempt_index", fmt.Sprintf("duplicates %s", existing))
			continue
		}
		seen[episode.TaskID][episode.AttemptIndex] = episode.ID
	}
	for _, taskID := range run.TaskIDs {
		for attemptIndex := 1; attemptIndex <= run.AttemptsPerTask; attemptIndex++ {
			if _, ok := seen[taskID][attemptIndex]; ok {
				continue
			}
			problems.add(displayPath(runDir, filepath.Join(runDir, "episodes")), "attempt_index", fmt.Sprintf("missing episode for task %q attempt %d", taskID, attemptIndex))
		}
	}
}

func validateEpisodeRecord(problems *collector, runDir string, path string, episode domain.Episode, run domain.RolloutRun) {
	rel := displayPath(runDir, path)
	problems.required(rel, "id", episode.ID)
	validatePathSegment(problems, rel, "id", episode.ID)
	problems.required(rel, "run_id", episode.RunID)
	if run.ID != "" && episode.RunID != run.ID {
		problems.add(rel, "run_id", fmt.Sprintf("got %q, want %q", episode.RunID, run.ID))
	}
	problems.required(rel, "task_id", episode.TaskID)
	validatePathSegment(problems, rel, "task_id", episode.TaskID)
	problems.requiredInt(rel, "attempt_index", episode.AttemptIndex)
	validateEpisodeStatus(problems, rel, "status", episode.Status)
	if episode.FailureClass != "" {
		validateFailureClass(problems, rel, "failure_class", episode.FailureClass)
	}
	validateEpisodeFailureLifecycle(problems, rel, episode)
	validateEpisodeTerminalFields(problems, rel, episode)
	validateAgentSpec(problems, rel, "agent", episode.Agent)
	validateModelSpec(problems, rel, "model", episode.Agent, episode.Model)
	validateSandboxSpec(problems, rel, "sandbox", episode.Sandbox)
	validateApprovalIsolation(problems, rel, episode.Agent, episode.Sandbox)
	validateSandboxRuntimeSourceRefs(problems, runDir, rel, episode.Sandbox.RuntimeSource)
	validateRunRef(problems, runDir, rel, "trajectory_path", episode.TrajectoryPath, true)
	validateRunRef(problems, runDir, rel, "agent_result_path", episode.AgentResultPath, true)
	validateRunRef(problems, runDir, rel, "verifier_result_path", episode.VerifierResultPath, true)
	validateRunRef(problems, runDir, rel, "reward_path", episode.RewardPath, true)
	validateRunRef(problems, runDir, rel, "artifact_dir", episode.ArtifactDir, true)
	validateRunRef(problems, runDir, rel, "artifact_manifest_path", episode.ArtifactManifestPath, false)
	validateArtifactRefs(problems, runDir, rel, "artifacts", episode.Artifacts)
}

func validateEpisodeTerminalFields(problems *collector, rel string, episode domain.Episode) {
	if !isTerminalEpisodeStatus(episode.Status) {
		return
	}
	if episode.FinishedAt == nil {
		problems.add(rel, "finished_at", "terminal episode requires finished_at")
	}
	if episode.CompletedAt == nil {
		problems.add(rel, "completed_at", "terminal episode requires completed_at")
	}
	if episode.DurationMS < 0 {
		problems.add(rel, "duration_ms", "must be non-negative")
	}
	if episode.Timing != nil && episode.Timing.TotalMS != episode.DurationMS {
		problems.add(rel, "timing.total_ms", fmt.Sprintf("got %d, want duration_ms %d", episode.Timing.TotalMS, episode.DurationMS))
	}
	if episode.Usage != nil {
		if episode.Usage.InputTokens < 0 {
			problems.add(rel, "usage.input_tokens", "must be non-negative")
		}
		if episode.Usage.OutputTokens < 0 {
			problems.add(rel, "usage.output_tokens", "must be non-negative")
		}
		if episode.Usage.TotalTokens < 0 {
			problems.add(rel, "usage.total_tokens", "must be non-negative")
		}
	}
}

func isTerminalEpisodeStatus(status domain.EpisodeStatus) bool {
	return status == domain.EpisodeStatusCompleted || status == domain.EpisodeStatusFailed
}

func validateEpisodeFailureLifecycle(problems *collector, path string, episode domain.Episode) {
	if episode.Status == domain.EpisodeStatusFailed {
		if episode.FailureClass == "" {
			problems.add(path, "failure_class", "failed episode requires a failure class")
		}
		return
	}
	if episode.FailureClass != "" {
		problems.add(path, "failure_class", "only failed episodes may set failure_class")
	}
}

func validateEpisodeFiles(problems *collector, runDir string, episodeDir string, episode domain.Episode) {
	var agent domain.AgentResult
	var agentOK bool
	agentPath := joinRunRef(runDir, episode.AgentResultPath, episodeDir, "agent.json")
	if agent, agentOK = readJSON[domain.AgentResult](problems, runDir, agentPath); agentOK {
		validateAgentResult(problems, runDir, agentPath, episode, agent)
	}
	var verifier domain.VerifierResult
	var verifierOK bool
	verifierPath := joinRunRef(runDir, episode.VerifierResultPath, episodeDir, "verifier.json")
	if verifier, verifierOK = readJSON[domain.VerifierResult](problems, runDir, verifierPath); verifierOK {
		validateVerifierResult(problems, runDir, verifierPath, episode, verifier)
	}
	var reward domain.Reward
	var rewardOK bool
	rewardPath := joinRunRef(runDir, episode.RewardPath, episodeDir, "reward.json")
	if reward, rewardOK = readJSON[domain.Reward](problems, runDir, rewardPath); rewardOK {
		validateReward(problems, runDir, rewardPath, episode, reward)
	}
	if agentOK && verifierOK && rewardOK {
		validateEpisodeOutcomeFiles(problems, runDir, episodeDir, episode, agent, verifier, reward)
	}
	trajectoryPath := joinRunRef(runDir, episode.TrajectoryPath, episodeDir, "trajectory.jsonl")
	validateTrajectory(problems, runDir, trajectoryPath)
	validateEpisodeArtifactManifest(problems, runDir, episodeDir, episode)
}

func validateEpisodeArtifactManifest(problems *collector, runDir string, episodeDir string, episode domain.Episode) {
	episodePath := filepath.Join(episodeDir, "episode.json")
	episodeRel := displayPath(runDir, episodePath)
	if strings.TrimSpace(episode.ArtifactManifestPath) == "" {
		if isTerminalEpisodeStatus(episode.Status) {
			problems.add(episodeRel, "artifact_manifest_path", "terminal episode requires artifact manifest")
		}
		return
	}
	manifestPath := joinRunRef(runDir, episode.ArtifactManifestPath, filepath.Join(episodeDir, "artifacts"), "manifest.json")
	manifest, ok := readJSON[domain.ArtifactManifest](problems, runDir, manifestPath)
	if !ok {
		return
	}
	validateArtifactManifest(problems, runDir, manifestPath, episode, manifest)
}

func validateArtifactManifest(problems *collector, runDir string, path string, episode domain.Episode, manifest domain.ArtifactManifest) {
	rel := displayPath(runDir, path)
	if manifest.SchemaVersion != "" && manifest.SchemaVersion != domain.LocalSchemaVersion {
		problems.add(rel, "schema_version", fmt.Sprintf("unsupported schema version %q", manifest.SchemaVersion))
	}
	problems.required(rel, "episode_id", manifest.EpisodeID)
	if manifest.EpisodeID != "" && episode.ID != "" && manifest.EpisodeID != episode.ID {
		problems.add(rel, "episode_id", fmt.Sprintf("got %q, want %q", manifest.EpisodeID, episode.ID))
	}
	if manifest.GeneratedAt.IsZero() {
		problems.add(rel, "generated_at", "is required")
	}
	for index, entry := range manifest.Entries {
		prefix := fmt.Sprintf("entries[%d]", index)
		validateRunRef(problems, runDir, rel, prefix+".path", entry.Path, true)
		if entry.Kind != "" {
			validateArtifactKind(problems, rel, prefix+".kind", entry.Kind)
		}
		if entry.Status == "" {
			problems.add(rel, prefix+".status", "is required")
		} else if !contract.IsArtifactManifestStatus(entry.Status) {
			problems.add(rel, prefix+".status", fmt.Sprintf("unsupported artifact manifest status %q", entry.Status))
		}
		if entry.Role != "" {
			validateArtifactRole(problems, rel, prefix+".role", entry.Role)
		}
		if entry.SizeBytes < 0 {
			problems.add(rel, prefix+".size_bytes", "must be greater than or equal to zero")
		}
		if entry.SHA256 != "" && !sha256Pattern.MatchString(entry.SHA256) {
			problems.add(rel, prefix+".sha256", "must be a 64-character lowercase hex digest")
		}
		if entry.MediaType != "" && !strings.Contains(entry.MediaType, "/") {
			problems.add(rel, prefix+".media_type", "must be a valid media type")
		}
	}
}

func validateAgentResult(problems *collector, runDir string, path string, episode domain.Episode, result domain.AgentResult) {
	rel := displayPath(runDir, path)
	validateAgentStatus(problems, rel, "status", result.Status)
	validateAgentExitReason(problems, rel, "exit_reason", result.ExitReason)
	if !contract.IsAgentLauncherKind(result.LauncherKind) {
		problems.add(rel, "launcher_kind", fmt.Sprintf("unsupported agent launcher kind %q", result.LauncherKind))
	}
	if result.RuntimeType != "" && !contract.IsAgentRuntimeType(result.RuntimeType) {
		problems.add(rel, "runtime_type", fmt.Sprintf("unsupported agent runtime type %q", result.RuntimeType))
	}
	if episodeRequiresFinalAgent(episode.Status) {
		validateFinalAgentStatus(problems, rel, "status", result.Status)
	}
	validateRunRef(problems, runDir, rel, "stdout_ref", result.StdoutRef, false)
	validateRunRef(problems, runDir, rel, "stderr_ref", result.StderrRef, false)
	validateRunRef(problems, runDir, rel, "raw_log_ref", result.RawLogRef, false)
	validateRunRef(problems, runDir, rel, "patch_ref", result.PatchRef, false)
	if result.RawLogRef != "" {
		validateExistingRunRef(problems, runDir, rel, "raw_log_ref", result.RawLogRef)
		validateAgentRawLog(problems, runDir, result.RawLogRef)
	}
	if result.PatchRef != "" {
		validateExistingRunRef(problems, runDir, rel, "patch_ref", result.PatchRef)
	}
	validateArtifactRefs(problems, runDir, rel, "artifacts", result.Artifacts)
}

func validateVerifierResult(problems *collector, runDir string, path string, episode domain.Episode, result domain.VerifierResult) {
	rel := displayPath(runDir, path)
	if result.Status != "" {
		validateEpisodeStatus(problems, rel, "status", result.Status)
	}
	if result.Type != "" {
		validateVerifierType(problems, rel, "type", result.Type)
	}
	if episode.Status == domain.EpisodeStatusCompleted && result.Status != domain.EpisodeStatusCompleted {
		problems.add(rel, "status", fmt.Sprintf("completed episode requires completed verifier result, got %q", result.Status))
	}
	validateArtifactRefs(problems, runDir, rel, "artifacts", result.Artifacts)
}

func validateReward(problems *collector, runDir string, path string, episode domain.Episode, reward domain.Reward) {
	rel := displayPath(runDir, path)
	validateRewardStatus(problems, rel, "status", reward.Status)
	if episode.Status == domain.EpisodeStatusCompleted || episode.Status == domain.EpisodeStatusFailed {
		if !reward.Final {
			problems.add(rel, "final", "terminal episode requires final reward")
		}
	}
}

func validateEpisodeOutcomeFiles(problems *collector, runDir string, episodeDir string, episode domain.Episode, agent domain.AgentResult, verifier domain.VerifierResult, reward domain.Reward) {
	episodeRel := displayPath(runDir, filepath.Join(episodeDir, "episode.json"))
	switch episode.Status {
	case domain.EpisodeStatusPending:
		validatePendingEpisodeFiles(problems, runDir, episodeDir, agent, verifier, reward)
	case domain.EpisodeStatusCompleted:
		if agent.Status == domain.AgentStatusFailed {
			problems.add(episodeRel, "status", "completed episode cannot have failed agent result")
		}
		if reward.Status == domain.RewardStatusPending {
			problems.add(episodeRel, "status", "completed episode cannot have pending reward")
		}
	case domain.EpisodeStatusFailed:
		validateFailedEpisodeFiles(problems, episodeRel, episode, agent, verifier, reward)
	}
}

func validatePendingEpisodeFiles(problems *collector, runDir string, episodeDir string, agent domain.AgentResult, verifier domain.VerifierResult, reward domain.Reward) {
	if agent.Status != domain.AgentStatusPending {
		problems.add(displayPath(runDir, filepath.Join(episodeDir, "agent.json")), "status", fmt.Sprintf("pending episode requires pending agent result, got %q", agent.Status))
	}
	if verifier.Status != domain.EpisodeStatusPending {
		problems.add(displayPath(runDir, filepath.Join(episodeDir, "verifier.json")), "status", fmt.Sprintf("pending episode requires pending verifier result, got %q", verifier.Status))
	}
	if reward.Status != domain.RewardStatusPending {
		problems.add(displayPath(runDir, filepath.Join(episodeDir, "reward.json")), "status", fmt.Sprintf("pending episode requires pending reward, got %q", reward.Status))
	}
}

func validateFailedEpisodeFiles(problems *collector, episodeRel string, episode domain.Episode, agent domain.AgentResult, verifier domain.VerifierResult, reward domain.Reward) {
	switch episode.FailureClass {
	case domain.FailureClassAgentFailed:
		if agent.Status != domain.AgentStatusFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("agent_failed episode requires failed agent result, got %q", agent.Status))
		}
		if reward.Status != domain.RewardStatusAgentFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("agent_failed episode requires agent_failed reward, got %q", reward.Status))
		}
	case domain.FailureClassPatchEmpty, domain.FailureClassPatchInvalid, domain.FailureClassTimeout:
		if agent.Status != domain.AgentStatusFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("%s episode requires failed agent result, got %q", episode.FailureClass, agent.Status))
		}
		if reward.Status != domain.RewardStatusAgentFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("%s episode requires agent_failed reward, got %q", episode.FailureClass, reward.Status))
		}
	case domain.FailureClassVerifierFailed:
		if verifier.Status != domain.EpisodeStatusFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("verifier_failed episode requires failed verifier result, got %q", verifier.Status))
		}
		if reward.Status == domain.RewardStatusPending || reward.Status == domain.RewardStatusAgentFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("verifier_failed episode has incompatible reward status %q", reward.Status))
		}
	case domain.FailureClassInfrastructure:
		if reward.Status != domain.RewardStatusInfraFailed {
			problems.add(episodeRel, "failure_class", fmt.Sprintf("infrastructure episode requires infra_failed reward, got %q", reward.Status))
		}
	}
}

func episodeRequiresFinalAgent(status domain.EpisodeStatus) bool {
	switch status {
	case domain.EpisodeStatusVerifying, domain.EpisodeStatusCompleted, domain.EpisodeStatusFailed:
		return true
	default:
		return false
	}
}
