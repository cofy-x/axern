package service

import (
	"context"
	"io"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type runtimeSpyFileService struct {
	handler *runtimeSpyHandler
}

func (s runtimeSpyFileService) recordOptions(options contract.HandlerOptions) {
	s.handler.fileOptions = append(s.handler.fileOptions, options)
}

func (s runtimeSpyFileService) StatFile(_ context.Context, _ *apipb.StatFileRequest, options contract.HandlerOptions) (*apipb.StatFileResponse, error) {
	s.recordOptions(options)
	if s.handler.statFileResponse != nil {
		return s.handler.statFileResponse, nil
	}
	return &apipb.StatFileResponse{}, nil
}

func (s runtimeSpyFileService) ListDir(_ context.Context, _ *apipb.ListDirRequest, options contract.HandlerOptions) (*apipb.ListDirResponse, error) {
	s.recordOptions(options)
	if s.handler.listDirResponse != nil {
		return s.handler.listDirResponse, nil
	}
	return &apipb.ListDirResponse{}, nil
}

func (s runtimeSpyFileService) ReadFile(_ context.Context, request *apipb.ReadFileRequest, options contract.HandlerOptions) (*apipb.ReadFileResponse, error) {
	s.recordOptions(options)
	s.handler.readFileRequests = append(s.handler.readFileRequests, request)
	return &apipb.ReadFileResponse{Data: []byte("hello")}, nil
}

func (s runtimeSpyFileService) WriteFile(_ context.Context, request *apipb.WriteFileRequest, options contract.HandlerOptions) (*apipb.WriteFileResponse, error) {
	s.recordOptions(options)
	s.handler.writeFileRequests = append(s.handler.writeFileRequests, request)
	return &apipb.WriteFileResponse{}, nil
}

func (s runtimeSpyFileService) Mkdir(_ context.Context, request *apipb.MkdirRequest, options contract.HandlerOptions) (*apipb.MkdirResponse, error) {
	s.recordOptions(options)
	s.handler.mkdirRequests = append(s.handler.mkdirRequests, request)
	return &apipb.MkdirResponse{}, nil
}

func (s runtimeSpyFileService) Remove(_ context.Context, request *apipb.RemoveRequest, options contract.HandlerOptions) (*apipb.RemoveResponse, error) {
	s.recordOptions(options)
	s.handler.removeRequests = append(s.handler.removeRequests, request)
	return &apipb.RemoveResponse{}, nil
}

func (s runtimeSpyFileService) Exists(_ context.Context, request *apipb.ExistsRequest, options contract.HandlerOptions) (*apipb.ExistsResponse, error) {
	s.recordOptions(options)
	s.handler.existsRequests = append(s.handler.existsRequests, request)
	return &apipb.ExistsResponse{Exists: true}, nil
}

func (s runtimeSpyFileService) Copy(_ context.Context, request *apipb.CopyRequest, options contract.HandlerOptions) (*apipb.CopyResponse, error) {
	s.recordOptions(options)
	s.handler.copyRequests = append(s.handler.copyRequests, request)
	return &apipb.CopyResponse{}, nil
}

func (s runtimeSpyFileService) Move(_ context.Context, request *apipb.MoveRequest, options contract.HandlerOptions) (*apipb.MoveResponse, error) {
	s.recordOptions(options)
	s.handler.moveRequests = append(s.handler.moveRequests, request)
	return &apipb.MoveResponse{}, nil
}

func (s runtimeSpyFileService) Chmod(_ context.Context, request *apipb.ChmodRequest, options contract.HandlerOptions) (*apipb.ChmodResponse, error) {
	s.recordOptions(options)
	s.handler.chmodRequests = append(s.handler.chmodRequests, request)
	return &apipb.ChmodResponse{}, nil
}

func (s runtimeSpyFileService) Touch(_ context.Context, request *apipb.TouchRequest, options contract.HandlerOptions) (*apipb.TouchResponse, error) {
	s.recordOptions(options)
	s.handler.touchRequests = append(s.handler.touchRequests, request)
	return &apipb.TouchResponse{}, nil
}

func (s runtimeSpyFileService) UploadArchive(_ context.Context, request *apipb.UploadArchiveRequest, input io.Reader, options contract.HandlerOptions) (*apipb.UploadArchiveResponse, error) {
	s.recordOptions(options)
	s.handler.uploadRequests = append(s.handler.uploadRequests, request)
	_, _ = io.Copy(io.Discard, input)
	return &apipb.UploadArchiveResponse{}, nil
}

func (s runtimeSpyFileService) DownloadArchive(_ context.Context, request *apipb.DownloadArchiveRequest, output io.Writer, options contract.HandlerOptions) (*apipb.DownloadArchiveResponse, error) {
	s.recordOptions(options)
	s.handler.downloadRequests = append(s.handler.downloadRequests, request)
	_, _ = output.Write([]byte("archive"))
	return &apipb.DownloadArchiveResponse{}, nil
}
