package exportdata

import (
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func buildRefs(runDir string, outputPath string, episode domain.Episode, taskPath string, agent domain.AgentResult) EpisodeRefs {
	episodeRoot := filepath.ToSlash(filepath.Join("episodes", episode.ID))
	return EpisodeRefs{
		RunDir:               runDirRef(outputPath, runDir),
		TaskPath:             runRelative(runDir, taskPath),
		EpisodePath:          runRelative(runDir, filepath.Join(runDir, "episodes", episode.ID, "episode.json")),
		AgentResultPath:      filepath.ToSlash(filepath.Join(episodeRoot, "agent.json")),
		VerifierResultPath:   filepath.ToSlash(filepath.Join(episodeRoot, "verifier.json")),
		RewardPath:           filepath.ToSlash(filepath.Join(episodeRoot, "reward.json")),
		TrajectoryPath:       filepath.ToSlash(filepath.Join(episodeRoot, "trajectory.jsonl")),
		RawLogRef:            agent.RawLogRef,
		PatchRef:             agent.PatchRef,
		ArtifactDir:          filepath.ToSlash(filepath.Join(episodeRoot, "artifacts")),
		ArtifactManifestPath: filepath.ToSlash(filepath.Join(episodeRoot, "artifacts", "manifest.json")),
		LLMTelemetryRef:      artifactRefByKind(agent.Artifacts, domain.ArtifactKindLLMTelemetry),
	}
}

func runDirRef(outputPath string, runDir string) string {
	outputDir := filepath.Dir(outputPath)
	rel, err := filepath.Rel(outputDir, runDir)
	if err != nil {
		abs, absErr := filepath.Abs(runDir)
		if absErr != nil {
			return filepath.ToSlash(filepath.Clean(runDir))
		}
		return filepath.ToSlash(abs)
	}
	if rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

func artifactRefByKind(artifacts []domain.ArtifactRef, kind domain.ArtifactKind) string {
	for _, artifact := range artifacts {
		if artifact.Kind == kind {
			return artifact.Path
		}
	}
	return ""
}

func joinRunRef(runDir string, ref string, fallback ...string) string {
	if ref != "" {
		return filepath.Join(runDir, filepath.FromSlash(ref))
	}
	parts := append([]string{runDir}, fallback...)
	return filepath.Join(parts...)
}

func runRelative(runDir string, path string) string {
	rel, err := filepath.Rel(runDir, path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(rel)
}
