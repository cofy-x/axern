package rollout

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func uploadVerifierAssets(ctx context.Context, instance sandbox.Instance, paths Paths, task domain.TaskInstance, appendStep func(domain.TrajectoryStep) (int, error)) error {
	for _, asset := range task.Verifier.Assets {
		if err := uploadVerifierAsset(ctx, instance, paths, task, asset, appendStep); err != nil {
			return err
		}
	}
	return nil
}

func uploadVerifierAsset(ctx context.Context, instance sandbox.Instance, paths Paths, task domain.TaskInstance, asset domain.VerifierAssetSpec, appendStep func(domain.TrajectoryStep) (int, error)) error {
	if task.InitialState != nil && task.InitialState.WorkspaceImage != nil {
		materializer, ok := instance.(sandbox.TaskAssetMaterializer)
		if !ok {
			return fmt.Errorf("sandbox does not support TaskSet asset materialization")
		}
		targetPath := asset.TargetPath
		if strings.TrimSpace(targetPath) == "" {
			targetPath = path.Join(task.Sandbox.Workdir, path.Base(asset.Path))
		}
		if err := materializer.MaterializeTaskAssets(ctx, asset.Path, targetPath, sandbox.TaskAssetKindVerifier); err != nil {
			return fmt.Errorf("materialize verifier asset %q: %w", asset.Path, err)
		}
		_, err := appendStep(domain.TrajectoryStep{
			Type:     domain.TrajectoryEventSystemWorkspaceUpload,
			Actor:    "rollout",
			Summary:  fmt.Sprintf("materialized verifier asset to %s", targetPath),
			InputRef: asset.Path,
		})
		return err
	}
	localPath, err := resolveRunPath(paths.ArtifactDir, asset.Path)
	if err != nil {
		return fmt.Errorf("resolve verifier asset %q: %w", asset.Path, err)
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat verifier asset %q: %w", asset.Path, err)
	}
	targetPath := verifierAssetTargetPath(task, asset, info.IsDir())
	if info.IsDir() {
		if err := instance.UploadDir(ctx, localPath, targetPath, sandbox.UploadDirOptions{}); err != nil {
			return fmt.Errorf("upload verifier asset %q: %w", asset.Path, err)
		}
	} else {
		if err := uploadVerifierAssetFile(ctx, instance, localPath, targetPath); err != nil {
			return fmt.Errorf("upload verifier asset %q: %w", asset.Path, err)
		}
	}
	step := domain.TrajectoryStep{
		Type:    domain.TrajectoryEventSystemWorkspaceUpload,
		Actor:   "rollout",
		Summary: fmt.Sprintf("uploaded verifier asset to %s", targetPath),
	}
	if inputRef := runRelativeInputRef(paths.ArtifactDir, localPath); inputRef != "" {
		step.InputRef = inputRef
	}
	_, err = appendStep(step)
	return err
}

func uploadVerifierAssetFile(ctx context.Context, instance sandbox.Instance, localPath string, targetPath string) error {
	tempDir, err := os.MkdirTemp("", "axrun-verifier-asset-*")
	if err != nil {
		return fmt.Errorf("create verifier asset upload directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()
	fileName := path.Base(targetPath)
	if fileName == "." || fileName == "/" {
		fileName = filepath.Base(localPath)
	}
	stagedPath := filepath.Join(tempDir, fileName)
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if err := copyFile(localPath, stagedPath, info.Mode().Perm()); err != nil {
		return err
	}
	return instance.UploadDir(ctx, tempDir, path.Dir(targetPath), sandbox.UploadDirOptions{})
}

func verifierAssetTargetPath(task domain.TaskInstance, asset domain.VerifierAssetSpec, isDir bool) string {
	if strings.TrimSpace(asset.TargetPath) != "" {
		targetPath := path.Clean(asset.TargetPath)
		if targetPath == "/" {
			return path.Join("/", path.Base(filepath.ToSlash(asset.Path)))
		}
		return targetPath
	}
	base := path.Base(filepath.ToSlash(asset.Path))
	cwd := strings.TrimSpace(task.Verifier.CWD)
	if cwd == "" {
		cwd = strings.TrimSpace(task.Sandbox.Workdir)
	}
	if cwd == "" {
		cwd = "/workspace"
	}
	target := path.Join(cwd, base)
	if isDir {
		return target
	}
	return target
}
