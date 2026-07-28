package service

import (
	"context"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxcontrol"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (h *sandboxService) configureSandboxControl() {
	h.sandboxController = sandboxcontrol.NewController(h.sandboxControlOptions())
}

func (h *sandboxService) List(ctx context.Context, request *runtime.ListContainersRequest) (*runtime.ListContainersResponse, error) {
	resp, err := h.sandboxControl().List(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Stats(ctx context.Context, request *runtime.StatsRequest) (*runtime.StatsResponse, error) {
	resp, err := h.sandboxControl().Stats(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Wait(ctx context.Context, request *runtime.WaitRequest) (*runtime.WaitResponse, error) {
	resp, err := h.sandboxControl().Wait(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Kill(ctx context.Context, request *runtime.KillRequest) (*runtime.KillResponse, error) {
	resp, err := h.sandboxControl().Kill(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Checkpoint(ctx context.Context, request *runtime.CheckpointRequest) (*runtime.CheckpointResponse, error) {
	return h.sandboxControl().Checkpoint(ctx, request)
}

func (h *sandboxService) sandboxControlOptions() sandboxcontrol.Options {
	return sandboxcontrol.Options{
		ListContainers: func(options ...container.ListOption) []*container.Container {
			return h.containerManager.List(options...)
		},
		GetContainer: func(id string) (*container.Container, error) {
			return h.containerManager.Get(id)
		},
		RuntimeCgroupPath: func(id string) (string, error) {
			return h.containerManager.RuntimeCgroupPath(id)
		},
		ContainerTarget: h.sandboxTargetResolver().Container,
		RunningTarget:   h.sandboxTargetResolver().Running,
	}
}

func (h *sandboxService) sandboxControl() *sandboxcontrol.Controller {
	if h.sandboxController != nil {
		return h.sandboxController
	}
	return sandboxcontrol.NewController(h.sandboxControlOptions())
}
