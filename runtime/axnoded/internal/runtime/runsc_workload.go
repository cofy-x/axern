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
		return contract.Exit{}, fmt.Errorf("wait for sandboxd workload: %w: %w", err, contract.ErrExitStatusUnavailable)
	}
	exit, err := workloadExit(result)
	if err != nil {
		return contract.Exit{}, fmt.Errorf("wait for sandboxd workload: %w", err)
	}
	return exit, nil
}

func workloadExit(result wire.WorkloadExitResponse) (contract.Exit, error) {
	exitedAt := result.ExitedAt.UTC()
	if exitedAt.IsZero() {
		return contract.Exit{}, fmt.Errorf("sandboxd workload result has no exit timestamp: %w", contract.ErrExitStatusUnavailable)
	}
	return contract.Exit{Status: result.ExitCode, Timestamp: exitedAt}, nil
}
