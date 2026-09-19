package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

const DefaultCreateWaitTimeout = 5 * time.Minute

type WaitTarget string

const (
	WaitTargetRunning        WaitTarget = "running"
	WaitTargetTerminal       WaitTarget = "terminal"
	WaitTargetRootfsSnapshot WaitTarget = "rootfs snapshot"
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

	resp, err := c.Get(waitCtx, runID)
	if err != nil {
		return nil, err
	}
	last := resp.GetRun()
	if last == nil {
		return nil, fmt.Errorf("get run %s returned an empty response", runID)
	}
	if onUpdate != nil {
		onUpdate(last)
	}
	if done, waitErr := runWaitResult(runID, target, last); done {
		return last, waitErr
	}

	watch, err := c.client.WatchRun(waitCtx, &runv1.WatchRunRequest{RunID: runID, AfterVersion: last.GetVersion()})
	if err != nil {
		if waitCtx.Err() != nil {
			return last, runWaitTimeoutError(waitCtx, runID, target)
		}
		return last, err
	}
	for {
		update, recvErr := watch.Recv()
		if recvErr != nil {
			if waitCtx.Err() != nil {
				return last, runWaitTimeoutError(waitCtx, runID, target)
			}
			if errors.Is(recvErr, io.EOF) {
				return last, fmt.Errorf("run %s watch ended before reaching %s", runID, target)
			}
			return last, recvErr
		}
		if update.GetRun() == nil {
			return last, fmt.Errorf("watch run %s returned an empty response", runID)
		}
		last = update.GetRun()
		if onUpdate != nil {
			onUpdate(last)
		}
		if done, waitErr := runWaitResult(runID, target, last); done {
			return last, waitErr
		}
	}
}

func runWaitResult(runID string, target WaitTarget, run *runv1.Run) (bool, error) {
	if run == nil {
		return false, nil
	}
	if target == WaitTargetRootfsSnapshot {
		result := run.GetRootfsSnapshot()
		if result == nil {
			return true, fmt.Errorf("run %s did not request a rootfs snapshot", runID)
		}
		switch result.GetStatus() {
		case runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_READY:
			return true, nil
		case runv1.RootfsSnapshotStatus_ROOTFS_SNAPSHOT_STATUS_FAILED:
			message := strings.TrimSpace(result.GetMessage())
			if message == "" {
				return true, fmt.Errorf("run %s rootfs snapshot failed", runID)
			}
			return true, fmt.Errorf("run %s rootfs snapshot failed: %s", runID, message)
		}
		if runFailed(run) {
			return true, runWaitFailure(runID, run)
		}
		return false, nil
	}
	if runFailed(run) {
		return true, runWaitFailure(runID, run)
	}
	if runSucceeded(run) {
		return true, nil
	}
	if target == WaitTargetRunning && run.GetStatus() == runv1.RunStatus_RUN_STATUS_RUNNING {
		return true, nil
	}
	if target == WaitTargetTerminal && runTerminal(run) {
		return true, nil
	}
	return false, nil
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
