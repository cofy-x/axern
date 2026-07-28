package runtime

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/startupflow"
)

func (r *RunscServiceHandler) startRunWithExitState(stdoutPath, stderrPath, bundlePath, containerID string, overlayArgs []string) (<-chan error, error) {
	runArgs := r.lifecycleArgs()
	runArgs = append(runArgs, overlayArgs...)
	runArgs = append(runArgs, "run", "--pid-file", r.common.RuntimePIDFilePath(containerID), "--bundle", bundlePath, containerID)
	return startRuntimeWaitLocked(r.waitLock(containerID), func() (<-chan error, error) {
		return r.common.StartRunWithExitState(stdoutPath, stderrPath, containerID, r.common.CommandArgs(runArgs...))
	})
}

func (r *RunscServiceHandler) createExecutionEnvelope(ctx context.Context, bundlePath, containerID string, overlayArgs []string) error {
	pidFilePath, _, err := r.common.PrepareContainerStatePaths(containerID)
	if err != nil {
		return err
	}

	args := r.lifecycleArgs()
	args = append(args, overlayArgs...)
	args = append(args, "create", "--pid-file", pidFilePath, "--bundle", bundlePath, containerID)
	_, err = r.common.Run(ctx, args...)
	return err
}

func (r *RunscServiceHandler) waitForContainerStart(ctx context.Context, containerID string, runWait <-chan error) error {
	return r.waitForStartup(ctx, containerID, runWait)
}

func (r *RunscServiceHandler) waitForEnvelopeStart(ctx context.Context, containerID string) error {
	return r.waitForStartup(ctx, containerID, nil)
}

func (r *RunscServiceHandler) waitForStartup(ctx context.Context, containerID string, runWait <-chan error) error {
	return startupflow.Wait(ctx, startupflow.Options{
		RuntimeName: "runsc",
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

func (r *RunscServiceHandler) isStartupReady(ctx context.Context, containerID string) bool {
	state, err := r.state(ctx, containerID)
	return err == nil && state.Status == string(contract.ContainerStatusRunning)
}
