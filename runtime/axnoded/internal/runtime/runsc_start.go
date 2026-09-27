package runtime

import (
	"context"
	"fmt"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/startupflow"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func (r *RunscServiceHandler) createPreparedContainer(ctx context.Context, stdoutPath, stderrPath, bundlePath, containerID string, overlayArgs []string, options contract.HandlerOptions) error {
	pidFilePath, _, err := r.common.PrepareContainerStatePaths(containerID)
	if err != nil {
		return err
	}

	args, err := r.preparedContainerCreateArgs(options, overlayArgs, pidFilePath, bundlePath, containerID)
	if err != nil {
		return err
	}
	return r.common.RunWithIO(ctx, stdoutPath, stderrPath, args...)
}

func (r *RunscServiceHandler) preparedContainerCreateArgs(options contract.HandlerOptions, overlayArgs []string, pidFilePath, bundlePath, containerID string) ([]string, error) {
	args, err := r.preparedContainerLifecycleArgs(options, containerID)
	if err != nil {
		return nil, err
	}
	args = append(args, overlayArgs...)
	return append(args, "create", "--pid-file", pidFilePath, "--bundle", bundlePath, containerID), nil
}

func (r *RunscServiceHandler) preparedContainerLifecycleArgs(options contract.HandlerOptions, containerID string, commandArgs ...string) ([]string, error) {
	args := r.lifecycleArgs()
	if options.NetworkMode == commonv1.NetworkMode_NETWORK_MODE_ISOLATED {
		if options.NetworkNamespacePath != "" {
			return nil, fmt.Errorf("isolated allocation %s has a connected network namespace", containerID)
		}
		// Both create and start consume runsc's network mode. start configures
		// netstack, so it must use the same loopback-only mode as create.
		args = append(args, "--network=none")
	}
	return append(args, commandArgs...), nil
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
