// Package oracle provides an agent harness that applies a task's reference
// solution (oracle) inside the sandbox without involving an LLM. It supports
// the oracle types expressed by native task records:
//
//   - reference_patch_file: git-apply a .patch file from a local path
//   - reference_patch_inline: git-apply patch content embedded in Command
//   - solution_file: upload a local directory or file to the sandbox workdir
//   - command (empty type or "command"): exec an arbitrary shell command
package oracle

import (
	"context"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/runref"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

const (
	oracleTypeReferencePatchFile   = "reference_patch_file"
	oracleTypeReferencePatchInline = "reference_patch_inline"
	oracleTypeSolutionFile         = "solution_file"
	oracleTypeCommand              = "command"

	remotePatchPath = ".axrun/oracle/reference.patch"
)

// Harness implements agent.Harness for oracle (reference solution) runs.
// It reads the task's OracleSpec and applies the reference solution so the
// verifier can measure a theoretical upper-bound on scores.
type Harness struct{}

// New returns a new oracle Harness.
func New() *Harness {
	return &Harness{}
}

func (h *Harness) Preflight() error {
	return nil
}

func (h *Harness) Run(ctx context.Context, request agent.Request) (agent.Result, error) {
	oracle := request.Task.Oracle
	if oracle == nil {
		return agent.Result{
			Status:  domain.AgentStatusSkipped,
			Summary: "oracle: no oracle spec on task",
		}, nil
	}

	switch strings.TrimSpace(oracle.Type) {
	case oracleTypeReferencePatchFile:
		return h.applyReferencePatchFile(ctx, request, oracle.Path)
	case oracleTypeReferencePatchInline:
		return h.applyInlinePatch(ctx, request, oracle.Command)
	case oracleTypeSolutionFile:
		return h.applySolutionFile(ctx, request, oracle.Path)
	case oracleTypeCommand, "":
		return h.runCommand(ctx, request, oracle.Command)
	default:
		return agent.Result{}, fmt.Errorf("oracle: unsupported oracle type %q", oracle.Type)
	}
}

func (h *Harness) applyReferencePatchFile(ctx context.Context, request agent.Request, path string) (agent.Result, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return agent.Result{}, fmt.Errorf("oracle: reference_patch_file requires a non-empty path")
	}
	if request.Task.InitialState != nil && request.Task.InitialState.WorkspaceImage != nil {
		if err := materializeOracleAsset(ctx, request, path, pathpkg.Join(taskWorkdir(request.Task), remotePatchPath)); err != nil {
			return agent.Result{}, err
		}
		result, err := request.Sandbox.Exec(
			ctx,
			sandbox.ShellCommand(fmt.Sprintf("git apply %s", remotePatchPath)),
			sandbox.ExecOptions{CWD: taskWorkdir(request.Task)},
		)
		if err != nil {
			return agent.Result{}, err
		}
		exitCode := result.ExitCode
		status := domain.AgentStatusCompleted
		summary := "oracle: reference patch applied"
		errorMessage := ""
		if result.ExitCode != 0 {
			status = domain.AgentStatusFailed
			summary = fmt.Sprintf("oracle: git apply exited with status %d", result.ExitCode)
			errorMessage = strings.TrimSpace(result.Stderr)
		}
		return agent.Result{
			Status:   status,
			Summary:  summary,
			Error:    errorMessage,
			ExitCode: &exitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, nil
	}
	localPath, err := resolveLocalOraclePath(request, path)
	if err != nil {
		return agent.Result{}, err
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: read reference patch file %q: %w", path, err)
	}
	return h.applyPatchBytes(ctx, request, data)
}

func resolveLocalOraclePath(request agent.Request, path string) (string, error) {
	if filepath.IsAbs(path) || strings.TrimSpace(request.ArtifactDir) == "" {
		return path, nil
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("oracle: path %q must not escape run directory", path)
	}
	return filepath.Join(runref.RunDirFromArtifactDir(request.ArtifactDir), clean), nil
}

func (h *Harness) applyInlinePatch(ctx context.Context, request agent.Request, patchContent string) (agent.Result, error) {
	patchContent = strings.TrimSpace(patchContent)
	if patchContent == "" {
		return agent.Result{}, fmt.Errorf("oracle: reference_patch_inline requires non-empty Command (patch content)")
	}
	return h.applyPatchBytes(ctx, request, []byte(patchContent))
}

func (h *Harness) applyPatchBytes(ctx context.Context, request agent.Request, data []byte) (agent.Result, error) {
	tmp, err := os.MkdirTemp("", "axrun-oracle-patch-")
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Keep local and remote patch basenames aligned so upload+apply paths match.
	patchFile := filepath.Join(tmp, filepath.Base(remotePatchPath))
	if err := os.WriteFile(patchFile, data, 0o644); err != nil {
		return agent.Result{}, fmt.Errorf("oracle: write patch to temp dir: %w", err)
	}

	workdir := taskWorkdir(request.Task)
	remoteDir := pathpkg.Join(workdir, pathpkg.Dir(remotePatchPath))
	if err := request.Sandbox.UploadDir(ctx, tmp, remoteDir, sandbox.UploadDirOptions{}); err != nil {
		return agent.Result{}, fmt.Errorf("oracle: upload reference patch: %w", err)
	}

	cmd := sandbox.ShellCommand(fmt.Sprintf("git apply %s", remotePatchPath))
	result, err := request.Sandbox.Exec(ctx, cmd, sandbox.ExecOptions{CWD: workdir})
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: exec git apply: %w", err)
	}
	if result.ExitCode != 0 {
		exitCode := result.ExitCode
		return agent.Result{
			Status:   domain.AgentStatusFailed,
			Summary:  fmt.Sprintf("oracle: git apply exited with status %d", result.ExitCode),
			Error:    fmt.Sprintf("git apply failed (exit %d): %s", result.ExitCode, strings.TrimSpace(result.Stderr)),
			ExitCode: &exitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, nil
	}
	exitCode := result.ExitCode
	return agent.Result{
		Status:   domain.AgentStatusCompleted,
		Summary:  "oracle: reference patch applied",
		ExitCode: &exitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func (h *Harness) applySolutionFile(ctx context.Context, request agent.Request, path string) (agent.Result, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return agent.Result{}, fmt.Errorf("oracle: solution_file requires a non-empty path")
	}
	if request.Task.InitialState != nil && request.Task.InitialState.WorkspaceImage != nil {
		target := pathpkg.Join(taskWorkdir(request.Task), pathpkg.Base(filepath.ToSlash(path)))
		if err := materializeOracleAsset(ctx, request, path, target); err != nil {
			return agent.Result{}, err
		}
		return agent.Result{Status: domain.AgentStatusCompleted, Summary: "oracle: solution materialized into workspace"}, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: stat solution path %q: %w", path, err)
	}
	workdir := taskWorkdir(request.Task)
	if info.IsDir() {
		if err := request.Sandbox.UploadDir(ctx, path, workdir, sandbox.UploadDirOptions{}); err != nil {
			return agent.Result{}, fmt.Errorf("oracle: upload solution directory: %w", err)
		}
		return agent.Result{
			Status:  domain.AgentStatusCompleted,
			Summary: "oracle: solution directory uploaded to workspace",
		}, nil
	}
	// Single file: stage in a temp dir to upload, then exec to move into workdir.
	tmp, err := os.MkdirTemp("", "axrun-oracle-solution-")
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	name := filepath.Base(path)
	data, err := os.ReadFile(path)
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: read solution file: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, name), data, 0o644); err != nil {
		return agent.Result{}, fmt.Errorf("oracle: write solution file to temp dir: %w", err)
	}
	if err := request.Sandbox.UploadDir(ctx, tmp, workdir, sandbox.UploadDirOptions{}); err != nil {
		return agent.Result{}, fmt.Errorf("oracle: upload solution file: %w", err)
	}
	return agent.Result{
		Status:  domain.AgentStatusCompleted,
		Summary: fmt.Sprintf("oracle: solution file %q uploaded to workspace", name),
	}, nil
}

func materializeOracleAsset(ctx context.Context, request agent.Request, source, target string) error {
	allowed := false
	for _, capability := range request.Task.Capabilities {
		if capability == "oracle_assets" {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("oracle: task does not grant oracle_assets capability")
	}
	materializer, ok := request.Sandbox.(sandbox.TaskAssetMaterializer)
	if !ok {
		return fmt.Errorf("oracle: sandbox does not support TaskSet asset materialization")
	}
	if err := materializer.MaterializeTaskAssets(ctx, source, target, sandbox.TaskAssetKindOracle); err != nil {
		return fmt.Errorf("oracle: materialize asset: %w", err)
	}
	return nil
}

func (h *Harness) runCommand(ctx context.Context, request agent.Request, command string) (agent.Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return agent.Result{
			Status:  domain.AgentStatusSkipped,
			Summary: "oracle: no command configured",
		}, nil
	}
	workdir := taskWorkdir(request.Task)
	result, err := request.Sandbox.Exec(ctx, sandbox.ShellCommand(command), sandbox.ExecOptions{CWD: workdir})
	if err != nil {
		return agent.Result{}, fmt.Errorf("oracle: exec command: %w", err)
	}
	exitCode := result.ExitCode
	if result.ExitCode != 0 {
		return agent.Result{
			Status:   domain.AgentStatusFailed,
			Summary:  fmt.Sprintf("oracle: command exited with status %d", result.ExitCode),
			Error:    fmt.Sprintf("command exited with status %d", result.ExitCode),
			ExitCode: &exitCode,
			Stdout:   result.Stdout,
			Stderr:   result.Stderr,
		}, nil
	}
	return agent.Result{
		Status:   domain.AgentStatusCompleted,
		Summary:  "oracle: command completed",
		ExitCode: &exitCode,
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
	}, nil
}

func taskWorkdir(task domain.TaskInstance) string {
	if task.InitialState != nil && strings.TrimSpace(task.InitialState.Workdir) != "" {
		return task.InitialState.Workdir
	}
	if strings.TrimSpace(task.Sandbox.Workdir) != "" {
		return task.Sandbox.Workdir
	}
	return "/workspace"
}
