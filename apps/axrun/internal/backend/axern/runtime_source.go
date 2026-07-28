package axern

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/runref"
	"github.com/cofy-x/axern/apps/axrun/internal/runtimeimage"
)

func (a Adapter) resolveRuntimeSource(request backend.ExecuteRequest) (backend.ExecuteRequest, error) {
	source := request.Task.Sandbox.RuntimeSource
	if source == nil || source.Type != domain.SandboxRuntimeSourceDockerfile {
		return request, nil
	}
	if err := appendTrajectoryStep(request, a.Now, domain.TrajectoryStep{
		Type:     domain.TrajectoryEventSystemImageBuildStart,
		Actor:    "axern-adapter",
		Summary:  fmt.Sprintf("building runtime image from %s", source.Dockerfile),
		InputRef: source.Dockerfile,
	}); err != nil {
		return request, err
	}
	resolver := a.Images
	if resolver == nil {
		defaultResolver := runtimeimage.NewDockerResolverFromEnv()
		resolver = defaultResolver
	}
	runDir := runref.RunDirFromArtifactDir(request.Paths.ArtifactDir)
	result, err := resolver.Resolve(context.Background(), runtimeimage.Request{
		RunDir:  runDir,
		Task:    request.Task,
		Episode: request.Episode,
	})
	if err != nil {
		_ = appendTrajectoryStep(request, a.Now, domain.TrajectoryStep{
			Type:     domain.TrajectoryEventSystemImageBuildDone,
			Actor:    "axern-adapter",
			Summary:  fmt.Sprintf("runtime image build failed: %v", err),
			InputRef: source.Dockerfile,
		})
		return request, err
	}
	artifactRef, err := writeRuntimeImageBuildArtifact(request.Paths.ArtifactDir, result)
	if err != nil {
		return request, err
	}
	imageSource := &domain.SandboxRuntimeSourceSpec{
		Type:   domain.SandboxRuntimeSourceImage,
		Image:  result.Image,
		Origin: domain.SandboxRuntimeSourceOriginRuntimeImageBuild,
	}
	request.Episode.Sandbox.RuntimeSource = imageSource
	buildArtifact := domain.ArtifactRef{
		Path:        artifactRef,
		Kind:        domain.ArtifactKindRuntimeImageBuild,
		Role:        domain.ArtifactRoleDerived,
		Description: "runtime image build metadata",
		Producer:    "axrun",
	}
	request.Episode.Artifacts = append(request.Episode.Artifacts, buildArtifact)
	if err := appendTrajectoryStep(request, a.Now, domain.TrajectoryStep{
		Type:      domain.TrajectoryEventSystemImageBuildDone,
		Actor:     "axern-adapter",
		Summary:   fmt.Sprintf("runtime image built as %s", result.Image),
		InputRef:  source.Dockerfile,
		Artifacts: []domain.ArtifactRef{buildArtifact},
	}); err != nil {
		return request, err
	}
	resolvedTask := request.Task
	resolvedTask.Sandbox.RuntimeSource = imageSource
	request.Task = resolvedTask
	return request, nil
}

func appendTrajectoryStep(request backend.ExecuteRequest, nowFn func() time.Time, step domain.TrajectoryStep) error {
	stepIndex, err := request.Store.CountTrajectorySteps(request.Paths.TrajectoryPath)
	if err != nil {
		return err
	}
	step.Index = stepIndex + 1
	if step.EventID == "" {
		step.EventID = fmt.Sprintf("step-%06d", step.Index)
	}
	if step.Timestamp.IsZero() {
		if nowFn != nil {
			step.Timestamp = nowFn().UTC()
		} else {
			step.Timestamp = time.Now().UTC()
		}
	}
	return request.Store.AppendTrajectoryStep(request.Paths.TrajectoryPath, step)
}

func writeRuntimeImageBuildArtifact(artifactDir string, result runtimeimage.Result) (string, error) {
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	path := filepath.Join(artifactDir, "runtime-image-build.json")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode runtime image build artifact: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write runtime image build artifact: %w", err)
	}
	return runref.ArtifactPath(artifactDir, path), nil
}
