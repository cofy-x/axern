package runtime

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func (r *RunscServiceHandler) KillContainer(ctx context.Context, request *apipb.SignalContainerRequest, options contract.HandlerOptions) (*apipb.SignalContainerResponse, error) {
	_, err := r.runLifecycle(ctx, "kill", options.ContainerID, request.Signal)
	return &apipb.SignalContainerResponse{}, err
}

func (r *RunscServiceHandler) ListContainers(ctx context.Context, _ contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return r.common.ListContainers(ctx)
}

func (r *RunscServiceHandler) ContainerSpec(_ context.Context, options contract.HandlerOptions) (*spec.Spec, error) {
	return r.common.ContainerSpec(options.ContainerID)
}
