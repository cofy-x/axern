package exportdata

import (
	"path/filepath"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func buildRefs(runDir string, outputPath string, episode domain.Episode, taskPath string, agent domain.AgentResult) EpisodeRefs {
	return EpisodeRefs{
		RunDir:               runDirRef(outputPath, runDir),
		TaskPath:             runRelative(runDir, taskPath),
		EpisodePath:          runRelative(runDir, filepath.Join(runDir, "episodes", episode.ID, "episode.json")),
		AgentResultPath:      episode.AgentResultPath,
		VerifierResultPath:   episode.VerifierResultPath,
		RewardPath:           episode.RewardPath,
		TrajectoryPath:       episode.TrajectoryPath,
		RawLogRef:            agent.RawLogRef,
		PatchRef:             agent.PatchRef,
		ArtifactDir:          episode.ArtifactDir,
		ArtifactManifestPath: episode.ArtifactManifestPath,
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
