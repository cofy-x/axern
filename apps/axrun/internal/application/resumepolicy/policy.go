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
	ActionExecute             Action = "execute"
	ActionFinalizeInterrupted Action = "finalize_interrupted"
	ActionSkip                Action = "skip"
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
		decision.Action = ActionFinalizeInterrupted
		decision.Reason = ReasonRunning
	case domain.EpisodeStatusVerifying:
		decision.Action = ActionFinalizeInterrupted
		decision.Reason = ReasonVerifying
	case domain.EpisodeStatusCompleted, domain.EpisodeStatusFailed:
		switch {
		case episode.CompletedAt == nil:
			decision.Reason = ReasonTerminalIncomplete
		case !episodeSidecarsExist(layout):
			decision.Reason = ReasonTerminalMissingSidecar
		case !artifactManifestExists(layout):
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
	return fileExists(filepath.Join(layout.ArtifactDir, "manifest.json"))
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
