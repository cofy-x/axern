package verifier

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type Executor interface {
	Exec(context.Context, sandbox.ExecCommand, sandbox.ExecOptions) (sandbox.ExecResult, error)
}

func Run(ctx context.Context, executor Executor, spec domain.VerifierSpec) (domain.VerifierResult, error) {
	startedAt := time.Now().UTC()
	result := domain.VerifierResult{
		Status:     domain.EpisodeStatusCompleted,
		Type:       spec.Type,
		Command:    spec.Command,
		CWD:        spec.CWD,
		TimeoutSec: spec.TimeoutSec,
		StartedAt:  &startedAt,
	}

	switch spec.Type {
	case domain.VerifierTypeNone:
		result = finishVerifier(result, startedAt)
		return result, nil
	case domain.VerifierTypeShell:
		execResult, err := executor.Exec(ctx, sandbox.ShellCommand(spec.Command), sandbox.ExecOptions{
			CWD:     spec.CWD,
			Timeout: timeout(spec),
		})
		exitCode := execResult.ExitCode
		result.ExitCode = &exitCode
		result.Stdout = execResult.Stdout
		result.Stderr = execResult.Stderr
		if err != nil {
			return finishVerifier(result, startedAt), err
		}
		if execResult.ExitCode != 0 {
			result.Status = domain.EpisodeStatusFailed
			result.Error = fmt.Sprintf("command exited with status %d", execResult.ExitCode)
		}
		return finishVerifier(result, startedAt), nil
	default:
		result.Status = domain.EpisodeStatusFailed
		result.Error = fmt.Sprintf("unsupported verifier type %q", spec.Type)
		return finishVerifier(result, startedAt), nil
	}
}

func finishVerifier(result domain.VerifierResult, startedAt time.Time) domain.VerifierResult {
	finishedAt := time.Now().UTC()
	result.FinishedAt = &finishedAt
	result.DurationMS = finishedAt.Sub(startedAt).Milliseconds()
	return result
}

func timeout(spec domain.VerifierSpec) time.Duration {
	if spec.TimeoutSec <= 0 {
		return 30 * time.Second
	}
	return time.Duration(spec.TimeoutSec) * time.Second
}
