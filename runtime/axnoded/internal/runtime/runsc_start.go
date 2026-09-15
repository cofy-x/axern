package runtime

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/startupflow"
)

func (r *RunscServiceHandler) createPreparedContainer(ctx context.Context, stdoutPath, stderrPath, bundlePath, containerID string, overlayArgs []string) error {
	pidFilePath, _, err := r.common.PrepareContainerStatePaths(containerID)
	if err != nil {
		return err
	}

	args := r.lifecycleArgs()
	args = append(args, overlayArgs...)
	args = append(args, "create", "--pid-file", pidFilePath, "--bundle", bundlePath, containerID)
	return r.common.RunWithIO(ctx, stdoutPath, stderrPath, args...)
}

func (r *RunscServiceHandler) waitForPreparedContainerStart(ctx context.Context, containerID string) error {
	return startupflow.Wait(ctx, startupflow.Options{
		RuntimeName: "runsc",
		ContainerID: containerID,
		PIDFilePath: r.common.RuntimePIDFilePath(containerID),
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
