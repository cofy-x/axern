package runtime

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func (r *RuncServiceHandler) KillContainer(ctx context.Context, request *apipb.SignalContainerRequest, options contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	_, err := r.common.Run(ctx, "kill", options.ContainerID, request.Signal)
	return &apipb.SignalContainerResponse{}, err
}

func (r *RuncServiceHandler) ListContainers(ctx context.Context, options contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return r.common.ListContainers(ctx)
}

func (r *RuncServiceHandler) ContainerSpec(ctx context.Context, options contract.HandlerOptions) (*spec.Spec, error) {
	return r.common.ContainerSpec(options.ContainerID)
}

func (r *RuncServiceHandler) CheckpointContainer(request *apipb.CheckpointRequest) error {
	return errord.ErrNotImplemented
}
