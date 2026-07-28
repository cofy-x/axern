package sandboxaccess

import (
	"context"
	"io"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

func (a *Accessor) StatFile(ctx context.Context, request *runtime.StatFileRequest) (*runtime.StatFileResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().StatFile(ctx, &runtime.StatFileRequest{ID: request.GetID(), Path: request.GetPath()}, handlerOptions(target))
}

func (a *Accessor) ListDir(ctx context.Context, request *runtime.ListDirRequest) (*runtime.ListDirResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().ListDir(ctx, &runtime.ListDirRequest{ID: request.GetID(), Path: request.GetPath()}, handlerOptions(target))
}

func (a *Accessor) ReadFile(ctx context.Context, request *runtime.ReadFileRequest) (*runtime.ReadFileResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().ReadFile(ctx, request, handlerOptions(target))
}

func (a *Accessor) WriteFile(ctx context.Context, request *runtime.WriteFileRequest) (*runtime.WriteFileResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().WriteFile(ctx, request, handlerOptions(target))
}

func (a *Accessor) Mkdir(ctx context.Context, request *runtime.MkdirRequest) (*runtime.MkdirResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Mkdir(ctx, request, handlerOptions(target))
}

func (a *Accessor) Remove(ctx context.Context, request *runtime.RemoveRequest) (*runtime.RemoveResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Remove(ctx, request, handlerOptions(target))
}

func (a *Accessor) Exists(ctx context.Context, request *runtime.ExistsRequest) (*runtime.ExistsResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Exists(ctx, request, handlerOptions(target))
}

func (a *Accessor) Copy(ctx context.Context, request *runtime.CopyRequest) (*runtime.CopyResponse, error) {
	if request.GetID() == "" || request.GetSrcPath() == "" || request.GetDstPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Copy(ctx, request, handlerOptions(target))
}

func (a *Accessor) Move(ctx context.Context, request *runtime.MoveRequest) (*runtime.MoveResponse, error) {
	if request.GetID() == "" || request.GetSrcPath() == "" || request.GetDstPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Move(ctx, request, handlerOptions(target))
}

func (a *Accessor) Chmod(ctx context.Context, request *runtime.ChmodRequest) (*runtime.ChmodResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Chmod(ctx, request, handlerOptions(target))
}

func (a *Accessor) Touch(ctx context.Context, request *runtime.TouchRequest) (*runtime.TouchResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().Touch(ctx, request, handlerOptions(target))
}

func (a *Accessor) UploadArchive(ctx context.Context, request *runtime.UploadArchiveRequest, archive io.Reader) (*runtime.UploadArchiveResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().UploadArchive(ctx, request, archive, handlerOptions(target))
}

func (a *Accessor) DownloadArchive(ctx context.Context, request *runtime.DownloadArchiveRequest, archive io.Writer) (*runtime.DownloadArchiveResponse, error) {
	if request.GetID() == "" || request.GetPath() == "" {
		return nil, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(request.GetID())
	if err != nil {
		return nil, err
	}
	return target.Handler.FileService().DownloadArchive(ctx, request, archive, handlerOptions(target))
}
