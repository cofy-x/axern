package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

const DefaultCreateWaitTimeout = 5 * time.Minute

type WaitTarget string

const (
	WaitTargetRunning  WaitTarget = "running"
	WaitTargetTerminal WaitTarget = "terminal"
)

func ParseWaitTarget(value string, fallback WaitTarget) (WaitTarget, error) {
	switch target := WaitTarget(strings.ToLower(strings.TrimSpace(value))); target {
	case "":
		return fallback, nil
	case WaitTargetRunning, WaitTargetTerminal:
		return target, nil
	default:
		return "", fmt.Errorf("wait-for must be one of: running, terminal")
	}
}

func (c Control) Wait(ctx context.Context, runID string, target WaitTarget, timeout time.Duration, onUpdate func(*runv1.Run)) (*runv1.Run, error) {
	waitCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var last *runv1.Run
	for {
		resp, err := c.Get(waitCtx, runID)
		if err != nil {
			return last, err
		}
		last = resp.GetRun()
		if onUpdate != nil {
			onUpdate(last)
		}
		if last == nil {
			select {
			case <-waitCtx.Done():
				return last, runWaitTimeoutError(waitCtx, runID, target)
			case <-ticker.C:
			}
			continue
		}
		if runFailed(last) {
			return last, runWaitFailure(runID, last)
		}
		if runSucceeded(last) {
			return last, nil
		}
		if target == WaitTargetRunning && last.GetStatus() == runv1.RunStatus_RUN_STATUS_RUNNING {
			return last, nil
		}
		if target == WaitTargetTerminal && runTerminal(last) {
			return last, nil
		}
		select {
		case <-waitCtx.Done():
			return last, runWaitTimeoutError(waitCtx, runID, target)
		case <-ticker.C:
		}
	}
}

func runWaitTimeoutError(ctx context.Context, runID string, target WaitTarget) error {
	if ctx.Err() == context.Canceled {
		return ctx.Err()
	}
	return fmt.Errorf("timed out waiting for run %s to reach %s", runID, target)
}

func runWaitFailure(runID string, run *runv1.Run) error {
	message := strings.TrimSpace(run.GetMessage())
	if message == "" {
		return fmt.Errorf("run %s reached %s", runID, run.GetStatus().String())
	}
	return fmt.Errorf("run %s reached %s: %s", runID, run.GetStatus().String(), message)
}

func runSucceeded(run *runv1.Run) bool {
	if run == nil {
		return false
	}
	return run.GetStatus() == runv1.RunStatus_RUN_STATUS_SUCCEEDED
}

func runFailed(run *runv1.Run) bool {
	if run == nil {
		return false
	}
	switch run.GetStatus() {
	case runv1.RunStatus_RUN_STATUS_FAILED, runv1.RunStatus_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}

func runTerminal(run *runv1.Run) bool {
	if run == nil {
		return false
	}
	switch run.GetStatus() {
	case runv1.RunStatus_RUN_STATUS_SUCCEEDED, runv1.RunStatus_RUN_STATUS_FAILED, runv1.RunStatus_RUN_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}
