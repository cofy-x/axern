package runtime

import (
	"context"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
)

func (r *RunscServiceHandler) ExecContainer(
	ctx context.Context,
	request *apipb.ExecContainerRequest,
	options contract.HandlerOptions,
) (*apipb.ExecContainerResponse, error) {
	return runtimesandboxd.ExecContainer(ctx, request, options, r.common.ContainerRoot())
}

func (r *RunscServiceHandler) OpenExecSession(
	ctx context.Context,
	request *apipb.ExecSessionOpen,
	options contract.HandlerOptions,
) (contract.Session, error) {
	return runtimesandboxd.OpenExecSession(ctx, request, options, r.common.ContainerRoot())
}
