package localstore

import "github.com/cofy-x/axern/apps/axrun/internal/runref"

func runRelativePath(runDir string, path string) string {
	return runref.RunRelativePath(runDir, path)
}

func runDirFromArtifactDir(artifactDir string) string {
	return runref.RunDirFromArtifactDir(artifactDir)
}
