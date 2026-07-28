package runref

import (
	"path/filepath"
	"strings"
)

func RunDirFromArtifactDir(artifactDir string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(artifactDir)))
}

func RunRelativePath(runDir string, path string) string {
	rel, err := filepath.Rel(runDir, path)
	if err != nil {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(rel)
}

func ArtifactPath(artifactDir string, path string) string {
	if strings.TrimSpace(artifactDir) == "" {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return RunRelativePath(RunDirFromArtifactDir(artifactDir), path)
}
