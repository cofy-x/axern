package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

// CommandHarness executes an explicit command without provider credentials or
// managed LLM proxy semantics.
type CommandHarness struct{}

func (CommandHarness) Preflight() error { return nil }

func (CommandHarness) Run(ctx context.Context, request Request) (Result, error) {
	if request.Agent.Runtime == nil {
		return Result{}, fmt.Errorf("command agent runtime is required")
	}
	command := sandbox.ArgvCommand(request.Agent.Runtime.Command)
	if err := command.Validate(); err != nil {
		return Result{}, fmt.Errorf("command agent: %w", err)
	}
	startedAt := time.Now().UTC()
	result, err := request.Sandbox.Exec(ctx, command, sandbox.ExecOptions{
		CWD:     commandWorkdir(request),
		User:    request.Agent.Runtime.User,
		Timeout: durationSeconds(request.Agent.Runtime.TimeoutSec),
		Env:     cloneEnv(request.Agent.Runtime.Env),
	})
	finishedAt := time.Now().UTC()
	if err != nil {
		return Result{}, err
	}
	exitCode := result.ExitCode
	status := domain.AgentStatusCompleted
	summary := "command agent completed"
	errorText := ""
	exitReason := domain.AgentExitReasonCompleted
	if exitCode != 0 {
		status = domain.AgentStatusFailed
		summary = fmt.Sprintf("command agent exited with status %d", exitCode)
		errorText = summary
		exitReason = domain.AgentExitReasonCommandNonzero
	}
	return Result{
		Status:       status,
		Summary:      summary,
		Error:        errorText,
		ExitReason:   exitReason,
		LauncherKind: domain.AgentLauncherKindSandboxCommand,
		RuntimeType:  domain.AgentRuntimeTypeSandboxCommand,
		ExitCode:     &exitCode,
		Stdout:       result.Stdout,
		Stderr:       result.Stderr,
		StartedAt:    &startedAt,
		FinishedAt:   &finishedAt,
		DurationMS:   finishedAt.Sub(startedAt).Milliseconds(),
	}, nil
}

func commandWorkdir(request Request) string {
	if runtime := request.Agent.Runtime; runtime != nil && runtime.Workdir != "" {
		return runtime.Workdir
	}
	if request.Task.InitialState != nil && request.Task.InitialState.Workdir != "" {
		return request.Task.InitialState.Workdir
	}
	if request.Task.Sandbox.Workdir != "" {
		return request.Task.Sandbox.Workdir
	}
	return request.Episode.Sandbox.Workdir
}

func durationSeconds(value int) time.Duration {
	if value <= 0 {
		return 30 * time.Minute
	}
	return time.Duration(value) * time.Second
}
