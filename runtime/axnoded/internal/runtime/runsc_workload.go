package runtime

import (
	"context"
	"fmt"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

type sandboxdWorkloadClient interface {
	SignalWorkload(context.Context, string) error
	StopWorkload(context.Context, string) (wire.WorkloadExitResponse, error)
	WaitWorkload(context.Context) (wire.WorkloadExitResponse, error)
}

func (r *RunscServiceHandler) workloadClient(containerID string) (sandboxdWorkloadClient, error) {
	if containerID == "" {
		return nil, fmt.Errorf("sandboxd workload operation requires container id")
	}
	if r.newWorkloadClient == nil {
		return nil, fmt.Errorf("sandboxd workload client is unavailable")
	}
	return r.newWorkloadClient(runtimeoci.SandboxdBundleSocketPath(filepath.Join(r.containerRoot, containerID))), nil
}

func (r *RunscServiceHandler) KillContainer(ctx context.Context, request *apipb.SignalContainerRequest, options contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	client, err := r.workloadClient(options.ContainerID)
	if err != nil {
		return &apipb.SignalContainerResponse{}, err
	}
	err = client.SignalWorkload(ctx, request.GetSignal())
	return &apipb.SignalContainerResponse{}, err
}

func (r *RunscServiceHandler) StopWorkload(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	client, err := r.workloadClient(options.ContainerID)
	if err != nil {
		return contract.Exit{}, err
	}
	result, err := client.StopWorkload(ctx, "KILL")
	if err != nil {
		return contract.Exit{}, err
	}
	return workloadExit(result)
}

func (r *RunscServiceHandler) Wait(ctx context.Context, options contract.HandlerOptions) (contract.Exit, error) {
	client, err := r.workloadClient(options.ContainerID)
	if err != nil {
		return contract.Exit{}, err
	}
	result, err := client.WaitWorkload(ctx)
	if err != nil {
		return contract.Exit{}, r.classifyWorkloadWaitError(ctx, options.ContainerID, err)
	}
	exit, err := workloadExit(result)
	if err != nil {
		return contract.Exit{}, fmt.Errorf("wait for sandboxd workload: %w", err)
	}
	return exit, nil
}

// classifyWorkloadWaitError preserves the distinction between a temporarily
// unreachable sandboxd control socket and confirmed OCI termination without a
// recoverable workload result. Runtime liveness is only an observation, but it
// is sufficient to reject a false terminal classification: a running OCI
// sandbox cannot have an authoritative unavailable exit result. The monitor
// retries ordinary transport errors and commits ErrExitStatusUnavailable only
// after runsc or its durable exit checkpoint proves termination.
func (r *RunscServiceHandler) classifyWorkloadWaitError(ctx context.Context, containerID string, waitErr error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("wait for sandboxd workload: %w", ctxErr)
	}
	state, stateErr := r.state(ctx, containerID)
	if stateErr == nil && state.Status == string(contract.ContainerStatusExited) {
		return fmt.Errorf("wait for sandboxd workload after confirmed runtime exit: %w: %w", waitErr, contract.ErrExitStatusUnavailable)
	}
	if _, exited, exitErr := r.readExitState(containerID); exitErr == nil && exited {
		return fmt.Errorf("wait for sandboxd workload after durable runtime exit: %w: %w", waitErr, contract.ErrExitStatusUnavailable)
	}
	if stateErr != nil {
		if runtimeContainerAbsent(stateErr, containerID) {
			return fmt.Errorf("wait for sandboxd workload after confirmed runtime removal: %w: %w", waitErr, contract.ErrExitStatusUnavailable)
		}
		inventory, inventoryErr := r.ListContainers(ctx, contract.HandlerOptions{})
		if inventoryErr == nil {
			for _, candidate := range inventory {
				if candidate != nil && candidate.ID == containerID {
					return fmt.Errorf("wait for sandboxd workload: %w (runtime state unavailable: %v; inventory status: %s)", waitErr, stateErr, candidate.Status)
				}
			}
			return fmt.Errorf("wait for sandboxd workload after runtime disappeared from authoritative inventory: %w: %w", waitErr, contract.ErrExitStatusUnavailable)
		}
		return fmt.Errorf("wait for sandboxd workload: %w (runtime state unavailable: %v; runtime inventory unavailable: %v)", waitErr, stateErr, inventoryErr)
	}
	return fmt.Errorf("wait for sandboxd workload while runtime state is %q: %w", state.Status, waitErr)
}

func workloadExit(result wire.WorkloadExitResponse) (contract.Exit, error) {
	exitedAt := result.ExitedAt.UTC()
	if exitedAt.IsZero() {
		return contract.Exit{}, fmt.Errorf("sandboxd workload result has no exit timestamp: %w", contract.ErrExitStatusUnavailable)
	}
	return contract.Exit{Status: result.ExitCode, Timestamp: exitedAt}, nil
}
