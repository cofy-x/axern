package service

import (
	"context"
	"fmt"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/imageprocess"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (h *sandboxService) configureImageProcesses() {
	h.imageProcesses = imageprocess.NewController(h.imageProcessOptions())
}

func (h *sandboxService) ExecImage(ctx context.Context, request *runtime.ExecImageRequest) (*runtime.ExecImageResponse, error) {
	resp, err := h.imageProcessController().ExecImage(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ProcessImage(stream ProcessImageStreamServer) error {
	return errord.ToGRPC(h.imageProcessController().ProcessImage(stream))
}

func (h *sandboxService) imageProcessController() *imageprocess.Controller {
	if h.imageProcesses != nil {
		return h.imageProcesses
	}
	h.configureImageProcesses()
	return h.imageProcesses
}

func (h *sandboxService) imageProcessOptions() imageprocess.Options {
	return imageprocess.Options{
		InspectTarget:       h.inspectImageProcessTarget,
		EnsureRuntime:       h.ensureImageProcessRuntime,
		CreateContainer:     h.createImageProcessContainer,
		LoadContainerLabels: h.loadContainerLabels,
		DeleteContainer:     h.deleteImageProcessContainer,
		IgnoreDeleteError:   allocation.IsDeleteNotFound,
		Network:             h.config.NatBackend,
	}
}

func (h *sandboxService) inspectImageProcessTarget(ctx context.Context, parentID string) (imageprocess.Target, contract.RuntimeHandler, error) {
	target, err := h.sandboxTargetResolver().Running(parentID)
	if err != nil {
		return imageprocess.Target{}, nil, err
	}
	if target.Container == nil || target.Metadata == nil {
		return imageprocess.Target{}, nil, errord.ErrInvalidContainer
	}
	targetSpec, err := target.Handler.ContainerSpec(ctx, contract.HandlerOptions{
		ContainerID:     parentID,
		ContainerLabels: target.Labels(),
	})
	if err != nil {
		return imageprocess.Target{}, nil, fmt.Errorf("inspect target sandbox mounts: %w", err)
	}
	return imageprocess.Target{
		Runtime: target.RuntimeClass(),
		Spec:    targetSpec,
		Labels:  target.Labels(),
	}, target.Handler, nil
}

func (h *sandboxService) ensureImageProcessRuntime(ctx context.Context, template *runtime.RuntimeTemplate) (*langrtmanager.LanguageRuntime, error) {
	lrt, err := h.allocationController().PrepareRuntimeTemplate(ctx, template)
	if err != nil {
		return nil, err
	}
	lrt.SetExecutionEnvelopeEnabled(false)
	return lrt, nil
}

func (h *sandboxService) createImageProcessContainer(ctx context.Context, lrt *langrtmanager.LanguageRuntime, templateRequest, createRequest *runtime.CreateContainerRequest) error {
	_, _, err := h.allocationController().CreateRuntimeContainer(ctx, lrt, templateRequest, createRequest, nil, nil)
	return err
}

func (h *sandboxService) loadContainerLabels(containerID string) (map[string]string, error) {
	actorContainer, err := h.containerManager.Get(containerID)
	if err != nil || actorContainer == nil || actorContainer.Metadata == nil {
		return nil, fmt.Errorf("load container metadata: %w", errord.ErrFailedPrecondition)
	}
	return actorContainer.Metadata.GetLabels(), nil
}

func (h *sandboxService) deleteImageProcessContainer(ctx context.Context, containerID string) error {
	return h.allocationController().DeleteRuntimeContainer(ctx, containerID)
}
