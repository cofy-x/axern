package service

import (
	"context"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (h *sandboxService) ComputerUseStatus(ctx context.Context, request *runtime.ComputerUseStatusRequest) (*runtime.ComputerUseStatusResponse, error) {
	resp, err := h.sandboxAccessor().ComputerUseStatus(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ComputerUseScreenshot(ctx context.Context, request *runtime.ComputerUseScreenshotRequest) (*runtime.ComputerUseScreenshotResponse, error) {
	resp, err := h.sandboxAccessor().ComputerUseScreenshot(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ComputerUseDisplay(ctx context.Context, request *runtime.ComputerUseDisplayRequest) (*runtime.ComputerUseDisplayResponse, error) {
	resp, err := h.sandboxAccessor().ComputerUseDisplay(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ComputerUseMouse(ctx context.Context, request *runtime.ComputerUseMouseRequest) (*runtime.ComputerUseMouseResponse, error) {
	resp, err := h.sandboxAccessor().ComputerUseMouse(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ComputerUseKeyboard(ctx context.Context, request *runtime.ComputerUseKeyboardRequest) (*runtime.ComputerUseKeyboardResponse, error) {
	resp, err := h.sandboxAccessor().ComputerUseKeyboard(ctx, request)
	return resp, errord.ToGRPC(err)
}
