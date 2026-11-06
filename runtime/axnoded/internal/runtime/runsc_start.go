package runtime

import (
	"context"
	"fmt"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/startupflow"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

var (
	verifyCreatedCPUControls  = hostlinux.VerifyCgroupCPUControls
	verifyCreatedCPUProcesses = hostlinux.VerifyRunscCgroupProcesses
)

func (r *RunscServiceHandler) createPreparedContainer(ctx context.Context, stdoutPath, stderrPath, bundlePath, containerID string, overlayArgs []string, resource *apipb.LinuxContainerResources, options contract.HandlerOptions) error {
	pidFilePath, _, err := r.common.PrepareContainerStatePaths(containerID)
	if err != nil {
		return err
	}

	args, err := r.preparedContainerCreateArgs(options, overlayArgs, pidFilePath, bundlePath, containerID)
	if err != nil {
		return err
	}
	if err := r.common.RunWithIO(ctx, stdoutPath, stderrPath, args...); err != nil {
		return err
	}
	// runsc create may configure OCI resources, but has not started user code.
	// Re-read the same allocated leaf before publishing a prepared container.
	if !r.ignoreCgroups && (resource.GetCpuShares() > 0 || resource.GetCpuQuota() > 0 || resource.GetCpuPeriod() > 0) {
		if err := verifyCreatedCPUControls(options.RuntimeCgroupPath, resource); err != nil {
			return fmt.Errorf("verify CPU controls after runsc create: %w", err)
		}
		state, err := r.state(ctx, containerID)
		if err != nil {
			return fmt.Errorf("read runsc state for CPU enforcement: %w", err)
		}
		if state.Pid <= 0 {
			return fmt.Errorf("runsc state has no Sentry host pid for CPU enforcement")
		}
		if err := verifyCreatedCPUProcesses(options.RuntimeCgroupPath, state.Pid, r.common.Binary()); err != nil {
			return fmt.Errorf("verify runsc CPU cgroup attribution: %w", err)
		}
	}
	if r.hostTaskLimit > 0 {
		if err := r.verifyHostTaskLimits(ctx, containerID, options.CgroupPath, options.RuntimeCgroupPath, r.hostTaskLimit); err != nil {
			return fmt.Errorf("verify host task controls after runsc create: %w", err)
		}
	}
	return nil
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
	if r.guestTaskLimit > 0 {
		args = append(args, "--in-sandbox-cgroup=v2")
	}
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
