package runtime

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/startupflow"
)

func (r *RuncServiceHandler) startRunWithExitState(stdoutPath, stderrPath, bundlePath, containerID string) (<-chan error, error) {
	return startRuntimeWaitLocked(r.waitLock(containerID), func() (<-chan error, error) {
		return r.common.StartRunWithExitState(stdoutPath, stderrPath, containerID, r.common.CommandArgs(
			"run", "--keep", "--pid-file", r.common.RuntimePIDFilePath(containerID), "--bundle", bundlePath, containerID,
		))
	})
}

func (r *RuncServiceHandler) createExecutionEnvelope(ctx context.Context, bundlePath, containerID string) error {
	pidFilePath, _, err := r.common.PrepareContainerStatePaths(containerID)
	if err != nil {
		return err
	}
	_, err = r.common.Run(ctx, "create", "--pid-file", pidFilePath, "--bundle", bundlePath, containerID)
	return err
}

func (r *RuncServiceHandler) waitForContainerStart(ctx context.Context, containerID string, runWait <-chan error) error {
	return r.waitForStartup(ctx, containerID, runWait)
}

func (r *RuncServiceHandler) waitForEnvelopeStart(ctx context.Context, containerID string) error {
	return r.waitForStartup(ctx, containerID, nil)
}

func (r *RuncServiceHandler) waitForStartup(ctx context.Context, containerID string, runWait <-chan error) error {
	return startupflow.Wait(ctx, startupflow.Options{
		RuntimeName: "runc",
		ContainerID: containerID,
		PIDFilePath: r.common.RuntimePIDFilePath(containerID),
		WaitCh:      runWait,
		ReadyByState: func(callCtx context.Context) bool {
			return r.isStartupReady(callCtx, containerID)
		},
		ExitState: func() (contract.Exit, bool, error) {
			return r.readExitState(containerID)
		},
		UnreadableExit: startupflow.UnreadableExitError,
	})
}

func (r *RuncServiceHandler) isStartupReady(ctx context.Context, containerID string) bool {
	state, err := r.state(ctx, containerID)
	return err == nil && (state.Status == string(contract.ContainerStatusRunning) || state.Status == string(contract.ContainerStatusExited))
}
