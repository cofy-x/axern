package runtime

import (
	"context"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func (r *RunscServiceHandler) ListContainers(ctx context.Context, _ contract.HandlerOptions) ([]*contract.UnionContainerState, error) {
	return r.common.ListContainers(ctx)
}

func (r *RunscServiceHandler) ContainerSpec(_ context.Context, options contract.HandlerOptions) (*spec.Spec, error) {
	return r.common.ContainerSpec(options.ContainerID)
}
