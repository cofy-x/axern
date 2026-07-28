package localstore

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func (s Store) WriteArtifactManifest(artifactDir string, manifest domain.ArtifactManifest) (string, error) {
	if strings.TrimSpace(artifactDir) == "" {
		return "", fmt.Errorf("artifact directory is required")
	}
	path := filepath.Join(artifactDir, "manifest.json")
	if err := writeJSON(path, manifest); err != nil {
		return "", fmt.Errorf("write artifact manifest: %w", err)
	}
	return runRelativePath(runDirFromArtifactDir(artifactDir), path), nil
}
