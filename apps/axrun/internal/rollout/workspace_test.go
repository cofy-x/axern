package rollout

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestWorkspaceImageNeverUsesClientUpload(t *testing.T) {
	sb := &fakeSandbox{}
	task := domain.TaskInstance{InitialState: &domain.InitialStateSpec{
		Type: "taskset_workspace_image",
		WorkspaceImage: &domain.WorkspaceImageSourceSpec{
			Variants:   []domain.WorkspaceImageVariantSpec{{Format: "nydus", Image: "registry.example/taskset@sha256:abc"}},
			SourcePath: "tasks/example/workspace",
			Target:     "/workspace",
		},
	}}
	uploaded, err := uploadInitialWorkspace(context.Background(), sb, Paths{}, task, func(domain.TrajectoryStep) (int, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if uploaded || len(sb.uploadLocalPaths) != 0 {
		t.Fatalf("workspace image used client upload: uploaded=%v paths=%#v", uploaded, sb.uploadLocalPaths)
	}
}

func TestExecuteUploadsInitialWorkspaceBeforeAgent(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	taskDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "workspace", "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	layout.TaskInstance.InitialState = &domain.InitialStateSpec{
		Type:    "directory",
		Path:    taskDir,
		Workdir: "/workspace",
	}
	sb := &fakeSandbox{}
	harness := &recordingAgent{}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   harness,
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if sb.uploadLocalPath != filepath.Join(taskDir, "workspace") || sb.uploadRemotePath != "/workspace" {
		t.Fatalf("upload paths = %q %q", sb.uploadLocalPath, sb.uploadRemotePath)
	}
	if len(sb.uploadOptions) != 1 || !sb.uploadOptions[0].Writable {
		t.Fatalf("workspace upload options = %#v", sb.uploadOptions)
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if len(steps) < 4 || steps[2].Type != domain.TrajectoryEventSystemWorkspaceUpload || steps[3].Type != domain.TrajectoryEventAgentPlanned {
		t.Fatalf("steps = %#v", steps)
	}
	if steps[2].InputRef != "" {
		t.Fatalf("workspace upload input_ref = %q, want empty for external source", steps[2].InputRef)
	}
}

func TestExecuteFiltersExcludedInitialStatePathsBeforeUpload(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	taskDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(taskDir, "visible.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write visible: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "solution.sh"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write solution: %v", err)
	}
	if err := os.Mkdir(filepath.Join(taskDir, "solution"), 0o755); err != nil {
		t.Fatalf("mkdir solution dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "solution", "answer.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write nested solution: %v", err)
	}
	layout.TaskInstance.InitialState = &domain.InitialStateSpec{
		Type:         "directory",
		Path:         taskDir,
		Workdir:      "/workspace",
		ExcludePaths: []string{"solution.sh", "solution"},
	}
	sb := &fakeSandbox{uploadHook: func(path string) {
		if _, err := os.Stat(filepath.Join(path, "visible.txt")); err != nil {
			t.Fatalf("visible file missing from upload: %v", err)
		}
		if _, err := os.Stat(filepath.Join(path, "solution.sh")); !os.IsNotExist(err) {
			t.Fatalf("solution.sh leaked into upload: %v", err)
		}
		if _, err := os.Stat(filepath.Join(path, "solution", "answer.txt")); !os.IsNotExist(err) {
			t.Fatalf("solution directory leaked into upload: %v", err)
		}
	}}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   &recordingAgent{},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if sb.uploadLocalPath == taskDir {
		t.Fatalf("upload path = %q, want filtered temp dir", sb.uploadLocalPath)
	}
}

func TestExecuteUploadsVerifierAssetsAfterAgentWorkspace(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{
		Type:    domain.VerifierTypeShell,
		Command: "bash run-tests.sh",
		CWD:     "/workspace",
		Assets: []domain.VerifierAssetSpec{
			{Path: "inputs/task-dir/run-tests.sh", TargetPath: "/workspace/run-tests.sh"},
		},
	})
	runDir := filepath.Dir(filepath.Dir(filepath.Dir(layout.ArtifactDir)))
	inputDir := filepath.Join(runDir, "inputs", "task-dir")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "run-tests.sh"), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write verifier asset: %v", err)
	}
	taskDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(taskDir, "workspace"), 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "workspace", "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	layout.TaskInstance.InitialState = &domain.InitialStateSpec{
		Type:    "directory",
		Path:    taskDir,
		Workdir: "/workspace",
	}
	assetStaged := false
	sb := &fakeSandbox{uploadHook: func(path string) {
		if _, err := os.Stat(filepath.Join(path, "run-tests.sh")); err == nil {
			assetStaged = true
		}
	}}
	_, err := Execute(Request{
		Store:          store,
		Task:           layout.TaskInstance,
		Episode:        layout.Episode,
		Paths:          paths(layout),
		SandboxRuntime: fakeRuntime{sandbox: sb},
		AgentHarness:   &recordingAgent{},
		Now:            fixedNow,
		RuntimeName:    "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(sb.uploadLocalPaths) != 2 || sb.uploadLocalPaths[0] != filepath.Join(taskDir, "workspace") || sb.uploadRemotePaths[1] != "/workspace" {
		t.Fatalf("upload paths = %#v %#v", sb.uploadLocalPaths, sb.uploadRemotePaths)
	}
	if len(sb.uploadOptions) != 2 || sb.uploadOptions[1].NoOverwrite {
		t.Fatalf("verifier asset upload options = %#v", sb.uploadOptions)
	}
	if !assetStaged {
		t.Fatal("verifier asset was not staged for upload")
	}
	steps := readTrajectorySteps(t, layout.TrajectoryPath)
	if len(steps) < 6 ||
		steps[2].Type != domain.TrajectoryEventSystemWorkspaceUpload ||
		steps[3].Type != domain.TrajectoryEventAgentPlanned ||
		steps[6].Type != domain.TrajectoryEventSystemWorkspaceUpload ||
		steps[7].Type != domain.TrajectoryEventVerifierPlanned {
		t.Fatalf("steps = %#v", steps)
	}
}

func TestUploadExcludePathsMapsWorkspaceRelativePaths(t *testing.T) {
	statePath := filepath.Join("run", "inputs", "task", "tasks", "task-a")
	uploadPath := filepath.Join(statePath, "workspace")

	got := uploadExcludePaths(statePath, uploadPath, []string{
		"solution.sh",
		"workspace/solution.sh",
		"workspace/solution",
		"workspace/nested/solution.json",
	})

	want := []string{"solution.sh", "solution", "nested/solution.json"}
	if len(got) != len(want) {
		t.Fatalf("uploadExcludePaths = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("uploadExcludePaths = %#v, want %#v", got, want)
		}
	}
}

func TestUploadExcludePathsAllowsDotPrefixedWorkspaceNames(t *testing.T) {
	statePath := filepath.Join("run", "inputs", "task", "tasks", "task-a")
	uploadPath := filepath.Join(statePath, "..workspace")

	got := uploadExcludePaths(statePath, uploadPath, []string{"..workspace/solution.sh"})

	want := []string{"solution.sh"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("uploadExcludePaths = %#v, want %#v", got, want)
	}
}

func TestRunRelativeInputRefAllowsOnlyRunLocalPaths(t *testing.T) {
	runDir := t.TempDir()
	artifactDir := filepath.Join(runDir, "episodes", "ep1", "artifacts")
	localPath := filepath.Join(runDir, "tasks", "task-a", "workspace")
	externalPath := filepath.Join(t.TempDir(), "workspace")

	if got := runRelativeInputRef(artifactDir, localPath); got != "tasks/task-a/workspace" {
		t.Fatalf("runRelativeInputRef(local) = %q", got)
	}
	if got := runRelativeInputRef(artifactDir, externalPath); got != "" {
		t.Fatalf("runRelativeInputRef(external) = %q, want empty", got)
	}
}

func TestResolveRunPathRejectsEscapingRelativePath(t *testing.T) {
	artifactDir := filepath.Join(t.TempDir(), "episodes", "ep1", "artifacts")
	if _, err := resolveRunPath(artifactDir, "../outside"); err == nil {
		t.Fatal("resolveRunPath error = nil, want escape rejection")
	}
}
