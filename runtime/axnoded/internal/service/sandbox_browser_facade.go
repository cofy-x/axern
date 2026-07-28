package service

import (
	"context"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (h *sandboxService) BrowserStatus(ctx context.Context, request *runtime.BrowserStatusRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserStatus(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserOpen(ctx context.Context, request *runtime.BrowserOpenRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserOpen(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserClose(ctx context.Context, request *runtime.BrowserCloseRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserClose(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserNavigate(ctx context.Context, request *runtime.BrowserNavigateRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserNavigate(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserResize(ctx context.Context, request *runtime.BrowserResizeRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserResize(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserClick(ctx context.Context, request *runtime.BrowserClickRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserClick(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserType(ctx context.Context, request *runtime.BrowserTypeRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserType(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) BrowserWait(ctx context.Context, request *runtime.BrowserWaitRequest) (*runtime.BrowserStatusResponse, error) {
	resp, err := h.sandboxAccessor().BrowserWait(ctx, request)
	return resp, errord.ToGRPC(err)
}
