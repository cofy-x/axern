package oracle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

// fakeInstance is a minimal sandbox.Instance for unit tests.
type fakeInstance struct {
	execResult sandbox.ExecResult
	execErr    error
	execCalled bool
	lastCmd    string
	uploaded   []string
}

func (f *fakeInstance) Exec(_ context.Context, cmd sandbox.ExecCommand, _ sandbox.ExecOptions) (sandbox.ExecResult, error) {
	f.execCalled = true
	f.lastCmd = cmd.Shell()
	return f.execResult, f.execErr
}

func (f *fakeInstance) UploadDir(_ context.Context, localPath string, _ string, _ sandbox.UploadDirOptions) error {
	f.uploaded = append(f.uploaded, localPath)
	return nil
}

func (f *fakeInstance) DownloadPath(_ context.Context, _ string, _ string, _ sandbox.DownloadPathOptions) error {
	return nil
}

func (f *fakeInstance) State() (sandbox.State, error) {
	return sandbox.State{}, nil
}

func (f *fakeInstance) Close(_ context.Context) error {
	return nil
}

func makeRequest(task domain.TaskInstance, instance sandbox.Instance) agent.Request {
	return agent.Request{
		Task:    task,
		Sandbox: instance,
	}
}

func TestOracleHarnessSkipsWhenNoOracleSpec(t *testing.T) {
	h := New()
	task := domain.TaskInstance{ID: "task-1", Instruction: "Do it"}
	instance := &fakeInstance{}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusSkipped {
		t.Fatalf("status = %q, want skipped", result.Status)
	}
	if instance.execCalled {
		t.Fatal("exec should not be called when oracle spec is nil")
	}
}

func TestOracleHarnessRunsCommandForCommandType(t *testing.T) {
	h := New()
	task := domain.TaskInstance{
		ID:          "task-1",
		Instruction: "Do it",
		Sandbox:     domain.SandboxSpec{Workdir: "/workspace"},
		Oracle:      &domain.OracleSpec{Type: "command", Command: "echo hello"},
	}
	instance := &fakeInstance{execResult: sandbox.ExecResult{ExitCode: 0, Stdout: "hello\n"}}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.Stdout != "hello\n" {
		t.Fatalf("stdout = %q, want hello", result.Stdout)
	}
}

func TestOracleHarnessRunsCommandForEmptyType(t *testing.T) {
	h := New()
	task := domain.TaskInstance{
		ID:          "task-1",
		Instruction: "Do it",
		Oracle:      &domain.OracleSpec{Command: "true"},
	}
	instance := &fakeInstance{execResult: sandbox.ExecResult{ExitCode: 0}}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
}

func TestOracleHarnessSkipsCommandForEmptyTypeAndNoCommand(t *testing.T) {
	h := New()
	task := domain.TaskInstance{
		ID:     "task-1",
		Oracle: &domain.OracleSpec{},
	}
	instance := &fakeInstance{}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusSkipped {
		t.Fatalf("status = %q, want skipped", result.Status)
	}
}

func TestOracleHarnessAppliesReferencePatchFile(t *testing.T) {
	patchContent := `diff --git a/file.txt b/file.txt
--- a/file.txt
+++ b/file.txt
@@ -1 +1 @@
-old
+new
`
	tmpDir := t.TempDir()
	patchPath := filepath.Join(tmpDir, "fix.patch")
	if err := os.WriteFile(patchPath, []byte(patchContent), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	h := New()
	task := domain.TaskInstance{
		ID:      "repair-task",
		Oracle:  &domain.OracleSpec{Type: "reference_patch_file", Path: patchPath},
		Sandbox: domain.SandboxSpec{Workdir: "/workspace"},
	}
	instance := &fakeInstance{execResult: sandbox.ExecResult{ExitCode: 0}}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if len(instance.uploaded) == 0 {
		t.Fatal("expected patch dir upload, got none")
	}
	if !instance.execCalled {
		t.Fatal("expected git apply exec, got none")
	}
	wantCmd := "git apply " + remotePatchPath
	if instance.lastCmd != wantCmd {
		t.Fatalf("exec command = %q, want %q", instance.lastCmd, wantCmd)
	}
}

func TestOracleHarnessAppliesRunRelativeReferencePatchFile(t *testing.T) {
	patchContent := "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n"
	runDir := t.TempDir()
	patchPath := filepath.Join(runDir, "inputs", "task", "patches", "task-a", "reference.patch")
	if err := os.MkdirAll(filepath.Dir(patchPath), 0o755); err != nil {
		t.Fatalf("mkdir patch dir: %v", err)
	}
	if err := os.WriteFile(patchPath, []byte(patchContent), 0o644); err != nil {
		t.Fatalf("write patch: %v", err)
	}

	task := domain.TaskInstance{
		ID:      "repair-task",
		Oracle:  &domain.OracleSpec{Type: "reference_patch_file", Path: "inputs/task/patches/task-a/reference.patch"},
		Sandbox: domain.SandboxSpec{Workdir: "/workspace"},
	}
	instance := &fakeInstance{execResult: sandbox.ExecResult{ExitCode: 0}}
	request := makeRequest(task, instance)
	request.ArtifactDir = filepath.Join(runDir, "episodes", "episode-1", "artifacts")
	result, err := New().Run(context.Background(), request)
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusCompleted || len(instance.uploaded) == 0 {
		t.Fatalf("result = %#v uploaded=%#v", result, instance.uploaded)
	}
}

func TestOracleHarnessAppliesInlinePatch(t *testing.T) {
	patchContent := "--- a/f\n+++ b/f\n@@ -1 +1 @@\n-old\n+new\n"
	h := New()
	task := domain.TaskInstance{
		ID:     "repair-task",
		Oracle: &domain.OracleSpec{Type: "reference_patch_inline", Command: patchContent},
	}
	instance := &fakeInstance{execResult: sandbox.ExecResult{ExitCode: 0}}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	wantCmd := "git apply " + remotePatchPath
	if instance.lastCmd != wantCmd {
		t.Fatalf("exec command = %q, want %q", instance.lastCmd, wantCmd)
	}
}

func TestOracleHarnessUploadsSolutionDirectory(t *testing.T) {
	solutionDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(solutionDir, "answer.txt"), []byte("correct"), 0o644); err != nil {
		t.Fatalf("write solution: %v", err)
	}

	h := New()
	task := domain.TaskInstance{
		ID:      "oracle-task",
		Oracle:  &domain.OracleSpec{Type: "solution_file", Path: solutionDir},
		Sandbox: domain.SandboxSpec{Workdir: "/workspace"},
	}
	instance := &fakeInstance{}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if len(instance.uploaded) == 0 {
		t.Fatal("expected solution dir upload, got none")
	}
}

func TestOracleHarnessReturnsFailedOnNonZeroExit(t *testing.T) {
	exitCode := 1
	h := New()
	task := domain.TaskInstance{
		ID:     "task-1",
		Oracle: &domain.OracleSpec{Command: "false"},
	}
	instance := &fakeInstance{execResult: sandbox.ExecResult{ExitCode: exitCode, Stderr: "error"}}
	result, err := h.Run(context.Background(), makeRequest(task, instance))
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.Status != domain.AgentStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.ExitCode == nil || *result.ExitCode != 1 {
		t.Fatalf("exit code = %v, want 1", result.ExitCode)
	}
}

func TestOracleHarnessRejectsUnsupportedType(t *testing.T) {
	h := New()
	task := domain.TaskInstance{
		ID:     "task-1",
		Oracle: &domain.OracleSpec{Type: "unknown_type"},
	}
	instance := &fakeInstance{}
	_, err := h.Run(context.Background(), makeRequest(task, instance))
	if err == nil {
		t.Fatal("Run error = nil, want unsupported type error")
	}
}

func TestOraclePreflight(t *testing.T) {
	h := New()
	if err := h.Preflight(); err != nil {
		t.Fatalf("Preflight error = %v", err)
	}
}
