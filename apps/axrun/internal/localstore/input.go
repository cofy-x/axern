package localstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

type CapturedInputs struct {
	Input *domain.InputSpec
	Tasks []domain.TaskInstance
}

func (s Store) CaptureInputs(run RunLayout, input *domain.InputSpec, tasks []domain.TaskInstance, resolvedTaskSet *taskset.Resolved) (CapturedInputs, error) {
	capturedInput, err := captureTaskSetDescriptor(run, input, resolvedTaskSet)
	if err != nil {
		return CapturedInputs{}, err
	}
	capturedTasks := make([]domain.TaskInstance, 0, len(tasks))
	for _, task := range tasks {
		capturedTask, err := captureTaskInput(run, task)
		if err != nil {
			return CapturedInputs{}, err
		}
		capturedTasks = append(capturedTasks, capturedTask)
	}
	return CapturedInputs{Input: capturedInput, Tasks: capturedTasks}, nil
}

func captureTaskSetDescriptor(run RunLayout, input *domain.InputSpec, resolved *taskset.Resolved) (*domain.InputSpec, error) {
	if input == nil || input.Type != domain.InputTypeTaskSet || input.Format != domain.InputFormatTaskSet {
		return nil, fmt.Errorf("capture TaskSet descriptor: TaskSet input is required")
	}
	if resolved == nil {
		return nil, fmt.Errorf("capture TaskSet descriptor: resolved TaskSet is required")
	}
	data, err := json.MarshalIndent(resolved.Descriptor, "", "  ")
	if err != nil {
		return nil, err
	}
	destination := filepath.Join(run.InputsDir, "taskset-descriptor.json")
	if err := os.WriteFile(destination, append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	captured := *input
	captured.SourcePath = resolved.DescriptorPath
	captured.Path = runRelativePath(run.RunDir, destination)
	return &captured, nil
}

func captureTaskInput(run RunLayout, task domain.TaskInstance) (domain.TaskInstance, error) {
	task = captureTaskSource(task)
	var err error
	task, err = captureTaskOracle(run, task)
	if err != nil {
		return domain.TaskInstance{}, err
	}
	task, err = captureTaskVerifierAssets(run, task)
	if err != nil {
		return domain.TaskInstance{}, err
	}
	capturedInitialState, err := captureInitialState(run, task.ID, task.InitialState)
	if err != nil {
		return domain.TaskInstance{}, err
	}
	task.InitialState = capturedInitialState
	task = captureTaskRuntimeSource(task)
	return task, nil
}

func captureTaskSource(task domain.TaskInstance) domain.TaskInstance {
	if task.Source == nil || strings.TrimSpace(task.Source.Path) == "" {
		return task
	}
	source := *task.Source
	original := filepath.Clean(source.Path)
	source.SourcePath = original
	task.Source = &source
	return task
}

func captureTaskOracle(run RunLayout, task domain.TaskInstance) (domain.TaskInstance, error) {
	if task.Oracle == nil || strings.TrimSpace(task.Oracle.Path) == "" {
		return task, nil
	}
	oracle := *task.Oracle
	original := filepath.Clean(oracle.Path)
	if filepath.IsAbs(original) {
		destination := filepath.Join(run.TasksDir, task.ID, "input", "oracle", filepath.Base(original))
		if err := copyPath(original, destination); err != nil {
			return domain.TaskInstance{}, fmt.Errorf("capture task %q oracle %q: %w", task.ID, oracle.Path, err)
		}
		oracle.Path = runRelativePath(run.RunDir, destination)
	}
	task.Oracle = &oracle
	return task, nil
}

func captureTaskVerifierAssets(run RunLayout, task domain.TaskInstance) (domain.TaskInstance, error) {
	for index, asset := range task.Verifier.Assets {
		if strings.TrimSpace(asset.Path) == "" {
			continue
		}
		original := filepath.Clean(asset.Path)
		if filepath.IsAbs(original) {
			destination := filepath.Join(run.TasksDir, task.ID, "input", "verifier-assets", fmt.Sprintf("%03d-%s", index+1, filepath.Base(original)))
			if err := copyPath(original, destination); err != nil {
				return domain.TaskInstance{}, fmt.Errorf("capture task %q verifier asset %q: %w", task.ID, asset.Path, err)
			}
			task.Verifier.Assets[index].Path = runRelativePath(run.RunDir, destination)
		}
	}
	return task, nil
}

func captureInitialState(run RunLayout, taskID string, state *domain.InitialStateSpec) (*domain.InitialStateSpec, error) {
	if state == nil || strings.TrimSpace(state.Path) == "" {
		return state, nil
	}
	captured := *state
	sourcePath := filepath.Clean(captured.Path)
	destination := filepath.Join(run.TasksDir, taskID, "input", "initial-state")
	if err := copyPath(sourcePath, destination); err != nil {
		return nil, fmt.Errorf("capture task %q initial state: %w", taskID, err)
	}
	capturedRootRef := runRelativePath(run.RunDir, destination)
	capturedRootAbs := destination
	captured.Path = capturedRootRef
	if strings.TrimSpace(captured.Dockerfile) != "" {
		dockerfile := filepath.Clean(captured.Dockerfile)
		if sameOrWithin(dockerfile, sourcePath) {
			captured.Dockerfile = joinRunRef(capturedRootRef, relativeWithin(sourcePath, dockerfile))
		} else {
			destination := filepath.Join(capturedRootAbs, filepath.Base(dockerfile))
			if err := copyFile(dockerfile, destination); err != nil {
				return nil, fmt.Errorf("capture task %q Dockerfile: %w", taskID, err)
			}
			captured.Dockerfile = runRelativePath(run.RunDir, destination)
		}
	}
	return &captured, nil
}

func captureTaskRuntimeSource(task domain.TaskInstance) domain.TaskInstance {
	if task.Sandbox.RuntimeSource == nil {
		return task
	}
	runtimeSource := *task.Sandbox.RuntimeSource
	if strings.TrimSpace(runtimeSource.Dockerfile) != "" {
		if task.InitialState != nil && task.InitialState.Dockerfile != "" && runtimeSource.Origin == domain.SandboxRuntimeSourceOriginTaskInitialStateDockerfile {
			runtimeSource.Dockerfile = task.InitialState.Dockerfile
		}
	}
	task.Sandbox.RuntimeSource = &runtimeSource
	return task
}

func sameOrWithin(path string, root string) bool {
	rel, ok := relativePath(root, path)
	return ok && (rel == "" || rel != ".." && !strings.HasPrefix(rel, "../"))
}

func relativeWithin(root string, path string) string {
	rel, ok := relativePath(root, path)
	if !ok {
		return ""
	}
	return rel
}

func relativePath(root string, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	return filepath.ToSlash(rel), true
}

func joinRunRef(rootRef string, rel string) string {
	if strings.TrimSpace(rel) == "" {
		return filepath.ToSlash(filepath.Clean(rootRef))
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(rootRef), filepath.FromSlash(rel)))
}
