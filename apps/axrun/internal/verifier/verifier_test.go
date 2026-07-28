package verifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestRunNoneCompletesWithoutExec(t *testing.T) {
	executor := &fakeExecutor{}
	result, err := Run(context.Background(), executor, domain.VerifierSpec{Type: "none"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if executor.calls != 0 {
		t.Fatalf("exec calls = %d, want 0", executor.calls)
	}
	if result.Status != domain.EpisodeStatusCompleted || result.Type != "none" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunShellSuccess(t *testing.T) {
	executor := &fakeExecutor{result: sandbox.ExecResult{Stdout: "ok"}}
	result, err := Run(context.Background(), executor, domain.VerifierSpec{Type: "shell", Command: "printf ok", CWD: "/workspace", TimeoutSec: 9})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if executor.command.Shell() != "printf ok" || executor.options.CWD != "/workspace" || executor.options.Timeout != 9*time.Second {
		t.Fatalf("exec command/options = %#v %#v", executor.command, executor.options)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 || result.Stdout != "ok" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunShellNonzeroFailsWithoutError(t *testing.T) {
	executor := &fakeExecutor{result: sandbox.ExecResult{ExitCode: 5, Stderr: "nope"}}
	result, err := Run(context.Background(), executor, domain.VerifierSpec{Type: "shell", Command: "exit 5"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.EpisodeStatusFailed || result.Error == "" || result.Stderr != "nope" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunShellExecErrorIsInfrastructureError(t *testing.T) {
	execErr := errors.New("exec unavailable")
	_, err := Run(context.Background(), &fakeExecutor{err: execErr}, domain.VerifierSpec{Type: "shell", Command: "true"})
	if !errors.Is(err, execErr) {
		t.Fatalf("Run error = %v, want %v", err, execErr)
	}
}

func TestRunUnsupportedVerifierReturnsFailedResult(t *testing.T) {
	result, err := Run(context.Background(), &fakeExecutor{}, domain.VerifierSpec{Type: "python"})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.EpisodeStatusFailed || result.Type != "python" || result.Error == "" {
		t.Fatalf("result = %#v", result)
	}
}

type fakeExecutor struct {
	result  sandbox.ExecResult
	err     error
	calls   int
	command sandbox.ExecCommand
	options sandbox.ExecOptions
}

func (f *fakeExecutor) Exec(_ context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	f.calls++
	f.command = command
	f.options = options
	return f.result, f.err
}
