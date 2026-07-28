package rollout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/runref"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func uploadInitialWorkspace(ctx context.Context, instance sandbox.Instance, paths Paths, task domain.TaskInstance, appendStep func(domain.TrajectoryStep) (int, error)) (bool, error) {
	if task.InitialState == nil || task.InitialState.Type != "directory" || strings.TrimSpace(task.InitialState.Path) == "" {
		return false, nil
	}
	statePath, err := resolveRunPath(paths.ArtifactDir, task.InitialState.Path)
	if err != nil {
		return false, err
	}
	localPath := filepath.Join(statePath, "workspace")
	if info, err := os.Stat(localPath); err == nil {
		if !info.IsDir() {
			return false, fmt.Errorf("initial workspace path %s is not a directory", localPath)
		}
	} else if os.IsNotExist(err) {
		localPath = statePath
	} else {
		return false, fmt.Errorf("stat initial workspace path: %w", err)
	}
	uploadPath := localPath
	cleanupUploadPath := func() {}
	excludePaths := uploadExcludePaths(statePath, localPath, task.InitialState.ExcludePaths)
	if len(excludePaths) > 0 {
		filteredPath, cleanup, err := filteredUploadDir(localPath, excludePaths)
		if err != nil {
			return false, err
		}
		uploadPath = filteredPath
		cleanupUploadPath = cleanup
	}
	defer cleanupUploadPath()
	remotePath := task.InitialState.Workdir
	if remotePath == "" {
		remotePath = task.Sandbox.Workdir
	}
	if remotePath == "" {
		remotePath = "/workspace"
	}
	if err := instance.UploadDir(ctx, uploadPath, remotePath, sandbox.UploadDirOptions{Writable: true}); err != nil {
		return false, fmt.Errorf("upload initial workspace: %w", err)
	}
	step := domain.TrajectoryStep{
		Type:    domain.TrajectoryEventSystemWorkspaceUpload,
		Actor:   "rollout",
		Summary: fmt.Sprintf("uploaded initial workspace to %s", remotePath),
	}
	if inputRef := runRelativeInputRef(paths.ArtifactDir, localPath); inputRef != "" {
		step.InputRef = inputRef
	}
	_, err = appendStep(step)
	return true, err
}

func resolveRunPath(artifactDir string, path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("run-relative path %q must not escape the run directory", path)
	}
	return filepath.Join(runref.RunDirFromArtifactDir(artifactDir), clean), nil
}

func runRelativeInputRef(artifactDir string, path string) string {
	if strings.TrimSpace(artifactDir) == "" || strings.TrimSpace(path) == "" {
		return ""
	}
	rel := runref.RunRelativePath(runref.RunDirFromArtifactDir(artifactDir), path)
	if rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return ""
	}
	return rel
}
