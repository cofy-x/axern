package runtime

import (
	"context"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func (r *RunscServiceHandler) KillContainer(ctx context.Context, request *apipb.SignalContainerRequest, options contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	_, err := r.runLifecycle(ctx, "kill", options.ContainerID, request.Signal)
	return &apipb.SignalContainerResponse{}, err
}

func (r *RunscServiceHandler) ListContainers(ctx context.Context, options contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return r.common.ListContainers(ctx)
}

func (r *RunscServiceHandler) ContainerSpec(ctx context.Context, options contract.HandlerOptions) (*spec.Spec, error) {
	return r.common.ContainerSpec(options.ContainerID)
}

func (r *RunscServiceHandler) CheckpointContainer(request *apipb.CheckpointRequest) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(request.Timeout)*time.Second)
	defer cancel()
	args := []string{"checkpoint", "--image-path", request.CkptDir}
	if !request.Compress {
		args = append(args, "--leave-running")
	}
	args = append(args, request.ID)
	_, err := r.common.Run(ctx, args...)
	return err
}
