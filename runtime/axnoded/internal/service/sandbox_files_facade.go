package service

import (
	"context"
	"io"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (h *sandboxService) StatFile(ctx context.Context, request *runtime.StatFileRequest) (*runtime.StatFileResponse, error) {
	resp, err := h.sandboxAccessor().StatFile(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ListDir(ctx context.Context, request *runtime.ListDirRequest) (*runtime.ListDirResponse, error) {
	resp, err := h.sandboxAccessor().ListDir(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) ReadFile(ctx context.Context, request *runtime.ReadFileRequest) (*runtime.ReadFileResponse, error) {
	resp, err := h.sandboxAccessor().ReadFile(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) WriteFile(ctx context.Context, request *runtime.WriteFileRequest) (*runtime.WriteFileResponse, error) {
	resp, err := h.sandboxAccessor().WriteFile(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) MaterializeTaskAssets(_ context.Context, request *runtime.MaterializeTaskAssetsRequest) (*runtime.MaterializeTaskAssetsResponse, error) {
	durationMs, err := h.allocationController().MaterializeTaskAssets(request.GetID(), request.GetSourcePath(), request.GetTarget(), request.GetKind())
	return &runtime.MaterializeTaskAssetsResponse{DurationMs: durationMs}, errord.ToGRPC(err)
}

func (h *sandboxService) Mkdir(ctx context.Context, request *runtime.MkdirRequest) (*runtime.MkdirResponse, error) {
	resp, err := h.sandboxAccessor().Mkdir(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Remove(ctx context.Context, request *runtime.RemoveRequest) (*runtime.RemoveResponse, error) {
	resp, err := h.sandboxAccessor().Remove(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Exists(ctx context.Context, request *runtime.ExistsRequest) (*runtime.ExistsResponse, error) {
	resp, err := h.sandboxAccessor().Exists(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Copy(ctx context.Context, request *runtime.CopyRequest) (*runtime.CopyResponse, error) {
	resp, err := h.sandboxAccessor().Copy(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Move(ctx context.Context, request *runtime.MoveRequest) (*runtime.MoveResponse, error) {
	resp, err := h.sandboxAccessor().Move(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Chmod(ctx context.Context, request *runtime.ChmodRequest) (*runtime.ChmodResponse, error) {
	resp, err := h.sandboxAccessor().Chmod(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) Touch(ctx context.Context, request *runtime.TouchRequest) (*runtime.TouchResponse, error) {
	resp, err := h.sandboxAccessor().Touch(ctx, request)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) UploadArchive(ctx context.Context, request *runtime.UploadArchiveRequest, archive io.Reader) (*runtime.UploadArchiveResponse, error) {
	resp, err := h.sandboxAccessor().UploadArchive(ctx, request, archive)
	return resp, errord.ToGRPC(err)
}

func (h *sandboxService) DownloadArchive(ctx context.Context, request *runtime.DownloadArchiveRequest, archive io.Writer) (*runtime.DownloadArchiveResponse, error) {
	resp, err := h.sandboxAccessor().DownloadArchive(ctx, request, archive)
	return resp, errord.ToGRPC(err)
}
