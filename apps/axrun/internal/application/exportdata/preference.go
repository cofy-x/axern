package exportdata

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const (
	preferenceSchemaVersion = "axrun.export.preference"

	// maxPreferencePairsPerTask caps the cartesian product of chosen × rejected
	// arms to prevent combinatorial explosion when many attempts are used.
	maxPreferencePairsPerTask = 16
)

// exportPreference groups all export-ready episodes by task_id and emits one
// PreferenceRecord for each (chosen, rejected) pair found within a task.
// Tasks that only have passing or only failing episodes are skipped.
func exportPreference(runDir string, outputPath string, run domain.RolloutRun, episodes []domain.Episode) ([]any, error) {
	byTask := groupEpisodesByTask(episodes)
	var records []any
	for _, taskID := range sortedKeys(byTask) {
		taskEpisodes := byTask[taskID]
		taskPath := filepath.Join(runDir, "tasks", taskID, "task.json")
		task, err := readJSONFile[domain.TaskInstance](taskPath)
		if err != nil {
			return nil, fmt.Errorf("read task %q for preference export: %w", taskID, err)
		}
		pairs, err := buildPreferencePairs(runDir, outputPath, run, task, taskEpisodes)
		if err != nil {
			return nil, err
		}
		records = append(records, pairs...)
	}
	return records, nil
}

// buildPreferencePairs loads sidecar files for each episode in the task group,
// selects export-ready episodes, partitions them into chosen (verifier passed)
// and rejected (verifier failed with no infrastructure failure class), and emits
// (chosen × rejected) preference records up to maxPreferencePairsPerTask.
// Episodes that failed due to infrastructure problems, timeouts, or agent errors
// before the verifier ran are excluded from both arms so preference pairs reflect
// genuine differences in agent output quality rather than execution noise.
func buildPreferencePairs(runDir string, outputPath string, run domain.RolloutRun, task domain.TaskInstance, episodes []domain.Episode) ([]any, error) {
	var chosen, rejected []episodeBundle
	for _, episode := range episodes {
		bundle, err := loadEpisodeBundle(runDir, outputPath, run, episode)
		if err != nil {
			return nil, err
		}
		if !contract.IsEpisodeExportReady(bundle.Episode, bundle.Reward, bundle.Agent) {
			continue
		}
		switch {
		case bundle.Reward.Passed != nil && *bundle.Reward.Passed:
			chosen = append(chosen, bundle)
		case isPreferenceRejectedEpisode(bundle):
			// Verifier explicitly rejected the agent output; valid training signal.
			rejected = append(rejected, bundle)
			// Infra failures, timeouts, patch errors, etc. are excluded: Passed==nil
			// or failure class not tied to verifier means no quality judgment.
		}
	}
	if len(chosen) == 0 || len(rejected) == 0 {
		return nil, nil
	}
	var records []any
	count := 0
	for _, c := range chosen {
		for _, r := range rejected {
			if count >= maxPreferencePairsPerTask {
				break
			}
			records = append(records, buildPreferenceRecord(run, task, c, r))
			count++
		}
		if count >= maxPreferencePairsPerTask {
			break
		}
	}
	return records, nil
}

func isPreferenceRejectedEpisode(bundle episodeBundle) bool {
	if bundle.Reward.Passed == nil || *bundle.Reward.Passed {
		return false
	}
	if bundle.Episode.FailureClass == domain.FailureClassVerifierFailed {
		return true
	}
	if bundle.Episode.FailureClass != "" {
		return false
	}
	return bundle.Verifier.Status == domain.EpisodeStatusFailed
}

func buildPreferenceRecord(run domain.RolloutRun, task domain.TaskInstance, chosen episodeBundle, rejected episodeBundle) PreferenceRecord {
	return PreferenceRecord{
		SchemaVersion:       preferenceSchemaVersion,
		RecordID:            preferenceRecordID(task.ID, chosen.Episode.ID, rejected.Episode.ID),
		SourceSchemaVersion: run.SchemaVersion,
		RunID:               run.ID,
		TaskID:              task.ID,
		Instruction:         task.Instruction,
		Chosen:              episodeArm(chosen),
		Rejected:            episodeArm(rejected),
		Metadata:            preferenceMetadata(task),
	}
}

func episodeArm(bundle episodeBundle) EpisodeArm {
	return EpisodeArm{
		EpisodeID:    bundle.Episode.ID,
		AttemptIndex: bundle.Episode.AttemptIndex,
		Agent:        agentSummary(bundle.Episode.Agent),
		Model:        bundle.Episode.Model,
		Assistant:    bundle.Agent.Stdout,
		AgentStatus:  bundle.Agent.Status,
		ExitReason:   bundle.Agent.ExitReason,
		Reward:       rewardSummary(bundle.Reward),
		DurationMS:   bundle.Episode.DurationMS,
		Refs:         bundle.Refs,
	}
}

func preferenceRecordID(taskID string, chosenID string, rejectedID string) string {
	return fmt.Sprintf("preference_%s__%s__vs__%s", taskID, chosenID, rejectedID)
}

func preferenceMetadata(task domain.TaskInstance) domain.KeyValue {
	metadata := domain.KeyValue{"source": "axrun"}
	if task.Source != nil {
		metadata["task_source_type"] = string(task.Source.Type)
	}
	return metadata
}

func groupEpisodesByTask(episodes []domain.Episode) map[string][]domain.Episode {
	byTask := make(map[string][]domain.Episode)
	for _, episode := range episodes {
		byTask[episode.TaskID] = append(byTask[episode.TaskID], episode)
	}
	return byTask
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
