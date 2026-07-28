package contract

import (
	"context"
	"io"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

type ProcessService interface {
	OpenProcess(context.Context, *apipb.ProcessOpen, HandlerOptions) (Session, error)
}

type FileService interface {
	StatFile(context.Context, *apipb.StatFileRequest, HandlerOptions) (*apipb.StatFileResponse, error)
	ListDir(context.Context, *apipb.ListDirRequest, HandlerOptions) (*apipb.ListDirResponse, error)
	ReadFile(context.Context, *apipb.ReadFileRequest, HandlerOptions) (*apipb.ReadFileResponse, error)
	WriteFile(context.Context, *apipb.WriteFileRequest, HandlerOptions) (*apipb.WriteFileResponse, error)
	Mkdir(context.Context, *apipb.MkdirRequest, HandlerOptions) (*apipb.MkdirResponse, error)
	Remove(context.Context, *apipb.RemoveRequest, HandlerOptions) (*apipb.RemoveResponse, error)
	Exists(context.Context, *apipb.ExistsRequest, HandlerOptions) (*apipb.ExistsResponse, error)
	UploadArchive(context.Context, *apipb.UploadArchiveRequest, io.Reader, HandlerOptions) (*apipb.UploadArchiveResponse, error)
	DownloadArchive(context.Context, *apipb.DownloadArchiveRequest, io.Writer, HandlerOptions) (*apipb.DownloadArchiveResponse, error)
	Copy(context.Context, *apipb.CopyRequest, HandlerOptions) (*apipb.CopyResponse, error)
	Move(context.Context, *apipb.MoveRequest, HandlerOptions) (*apipb.MoveResponse, error)
	Chmod(context.Context, *apipb.ChmodRequest, HandlerOptions) (*apipb.ChmodResponse, error)
	Touch(context.Context, *apipb.TouchRequest, HandlerOptions) (*apipb.TouchResponse, error)
}
