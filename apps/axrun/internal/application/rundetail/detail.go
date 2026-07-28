package rundetail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/application/resumepolicy"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

type Result struct {
	RunID    string             `json:"run_id"`
	RunDir   string             `json:"run_dir"`
	Status   domain.RunStatus   `json:"status"`
	Summary  *domain.RunSummary `json:"summary,omitempty"`
	Run      domain.RolloutRun  `json:"run"`
	Plan     domain.RolloutPlan `json:"plan"`
	Episodes []EpisodeDetail    `json:"episodes"`
	Resume   ResumeSummary      `json:"resume"`
}

type ResumeSummary struct {
	ExecutableEpisodes int                     `json:"executable_episodes"`
	SkippedEpisodes    int                     `json:"skipped_episodes"`
	Decisions          []resumepolicy.Decision `json:"decisions"`
}

type EpisodeDetail struct {
	ID                   string                  `json:"id"`
	TaskID               string                  `json:"task_id"`
	AttemptIndex         int                     `json:"attempt_index"`
	Status               domain.EpisodeStatus    `json:"status"`
	FailureClass         domain.FailureClass     `json:"failure_class,omitempty"`
	Completed            bool                    `json:"completed"`
	ArtifactManifestPath string                  `json:"artifact_manifest_path,omitempty"`
	ArtifactManifest     ArtifactManifestSummary `json:"artifact_manifest"`
	Resume               resumepolicy.Decision   `json:"resume"`
	Refs                 EpisodeRefs             `json:"refs"`
	Episode              domain.Episode          `json:"episode"`
}

type EpisodeRefs struct {
	EpisodePath          string `json:"episode_path"`
	AgentResultPath      string `json:"agent_result_path,omitempty"`
	VerifierResultPath   string `json:"verifier_result_path,omitempty"`
	RewardPath           string `json:"reward_path,omitempty"`
	TrajectoryPath       string `json:"trajectory_path,omitempty"`
	ArtifactDir          string `json:"artifact_dir,omitempty"`
	ArtifactManifestPath string `json:"artifact_manifest_path,omitempty"`
}

type ArtifactManifestSummary struct {
	Path       string `json:"path,omitempty"`
	Exists     bool   `json:"exists"`
	EntryCount int    `json:"entry_count,omitempty"`
	Present    int    `json:"present,omitempty"`
	Missing    int    `json:"missing,omitempty"`
	Failed     int    `json:"failed,omitempty"`
	Error      string `json:"error,omitempty"`
}

func Load(runDir string) (Result, error) {
	loaded, err := localstore.LoadRun(runDir)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		RunID:   loaded.Layout.RolloutRun.ID,
		RunDir:  loaded.Layout.RunDir,
		Status:  loaded.Layout.RolloutRun.Status,
		Summary: loaded.Layout.RolloutRun.Summary,
		Run:     loaded.Layout.RolloutRun,
		Plan:    loaded.Plan,
	}
	for _, layout := range loaded.Episodes {
		decision := resumepolicy.Decide(layout)
		if decision.Action == resumepolicy.ActionExecute {
			result.Resume.ExecutableEpisodes++
		} else {
			result.Resume.SkippedEpisodes++
		}
		result.Resume.Decisions = append(result.Resume.Decisions, decision)
		result.Episodes = append(result.Episodes, episodeDetail(loaded.Layout.RunDir, layout, decision))
	}
	return result, nil
}

func episodeDetail(runDir string, layout localstore.EpisodeLayout, decision resumepolicy.Decision) EpisodeDetail {
	episode := layout.Episode
	return EpisodeDetail{
		ID:                   episode.ID,
		TaskID:               episode.TaskID,
		AttemptIndex:         episode.AttemptIndex,
		Status:               episode.Status,
		FailureClass:         episode.FailureClass,
		Completed:            episode.CompletedAt != nil,
		ArtifactManifestPath: episode.ArtifactManifestPath,
		ArtifactManifest:     artifactManifestSummary(runDir, episode),
		Resume:               decision,
		Refs: EpisodeRefs{
			EpisodePath:          filepath.ToSlash(filepath.Join("episodes", episode.ID, "episode.json")),
			AgentResultPath:      episode.AgentResultPath,
			VerifierResultPath:   episode.VerifierResultPath,
			RewardPath:           episode.RewardPath,
			TrajectoryPath:       episode.TrajectoryPath,
			ArtifactDir:          episode.ArtifactDir,
			ArtifactManifestPath: episode.ArtifactManifestPath,
		},
		Episode: episode,
	}
}

func artifactManifestSummary(runDir string, episode domain.Episode) ArtifactManifestSummary {
	summary := ArtifactManifestSummary{Path: episode.ArtifactManifestPath}
	if episode.ArtifactManifestPath == "" {
		return summary
	}
	manifestPath, ok := resumepolicy.RunRefPath(runDir, episode.ArtifactManifestPath)
	if !ok {
		summary.Error = "artifact manifest path must be run-root-relative"
		return summary
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		summary.Error = fmt.Sprintf("read artifact manifest: %v", err)
		return summary
	}
	var manifest domain.ArtifactManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		summary.Error = fmt.Sprintf("decode artifact manifest: %v", err)
		return summary
	}
	summary.Exists = true
	summary.EntryCount = len(manifest.Entries)
	for _, entry := range manifest.Entries {
		switch entry.Status {
		case domain.ArtifactManifestStatusPresent:
			summary.Present++
		case domain.ArtifactManifestStatusMissing:
			summary.Missing++
		case domain.ArtifactManifestStatusFailed:
			summary.Failed++
		}
	}
	return summary
}
