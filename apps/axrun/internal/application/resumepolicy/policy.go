package resumepolicy

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
)

type Action string

const (
	ActionExecute Action = "execute"
	ActionSkip    Action = "skip"
)

type Reason string

const (
	ReasonPending                 Reason = "pending"
	ReasonRunning                 Reason = "running"
	ReasonVerifying               Reason = "verifying"
	ReasonTerminalIncomplete      Reason = "terminal_incomplete"
	ReasonTerminalMissingSidecar  Reason = "terminal_missing_sidecar"
	ReasonTerminalMissingManifest Reason = "terminal_missing_manifest"
	ReasonTerminalComplete        Reason = "terminal_complete"
	ReasonUnsupportedStatus       Reason = "unsupported_status"
)

type Decision struct {
	EpisodeID    string               `json:"episode_id"`
	TaskID       string               `json:"task_id"`
	AttemptIndex int                  `json:"attempt_index"`
	Status       domain.EpisodeStatus `json:"status"`
	Action       Action               `json:"action"`
	Reason       Reason               `json:"reason"`
}

func Decide(layout localstore.EpisodeLayout) Decision {
	episode := layout.Episode
	decision := Decision{
		EpisodeID:    episode.ID,
		TaskID:       episode.TaskID,
		AttemptIndex: episode.AttemptIndex,
		Status:       episode.Status,
		Action:       ActionSkip,
		Reason:       ReasonUnsupportedStatus,
	}
	switch episode.Status {
	case domain.EpisodeStatusPending:
		decision.Action = ActionExecute
		decision.Reason = ReasonPending
	case domain.EpisodeStatusRunning:
		decision.Action = ActionExecute
		decision.Reason = ReasonRunning
	case domain.EpisodeStatusVerifying:
		decision.Action = ActionExecute
		decision.Reason = ReasonVerifying
	case domain.EpisodeStatusCompleted, domain.EpisodeStatusFailed:
		switch {
		case episode.CompletedAt == nil:
			decision.Action = ActionExecute
			decision.Reason = ReasonTerminalIncomplete
		case !episodeSidecarsExist(layout):
			decision.Action = ActionExecute
			decision.Reason = ReasonTerminalMissingSidecar
		case strings.TrimSpace(episode.ArtifactManifestPath) == "":
			decision.Action = ActionExecute
			decision.Reason = ReasonTerminalMissingManifest
		case !artifactManifestExists(layout):
			decision.Action = ActionExecute
			decision.Reason = ReasonTerminalMissingManifest
		default:
			decision.Action = ActionSkip
			decision.Reason = ReasonTerminalComplete
		}
	}
	return decision
}

func episodeSidecarsExist(layout localstore.EpisodeLayout) bool {
	for _, path := range []string{layout.AgentJSONPath, layout.VerifierJSONPath, layout.RewardJSONPath, layout.TrajectoryPath} {
		if !fileExists(path) {
			return false
		}
	}
	return true
}

func artifactManifestExists(layout localstore.EpisodeLayout) bool {
	runDir := filepath.Dir(filepath.Dir(layout.EpisodeDir))
	manifestPath, ok := RunRefPath(runDir, layout.Episode.ArtifactManifestPath)
	if !ok {
		return false
	}
	return fileExists(manifestPath)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func RunRefPath(runDir string, ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" || filepath.IsAbs(ref) {
		return "", false
	}
	clean := path.Clean(ref)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return filepath.Join(runDir, filepath.FromSlash(clean)), true
}
