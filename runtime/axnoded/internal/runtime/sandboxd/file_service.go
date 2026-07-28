package sandboxd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type fileClient interface {
	StatFile(context.Context, string) (FileStatResponse, error)
	ListDir(context.Context, string) (FileListResponse, error)
	ReadFile(context.Context, string) (FileReadResponse, error)
	Exists(context.Context, string) (FileExistsResponse, error)
	WriteFile(context.Context, FileWriteRequest) error
	Mkdir(context.Context, FileMkdirRequest) error
	Remove(context.Context, FileRemoveRequest) error
	Copy(context.Context, FileCopyRequest) error
	Move(context.Context, FileMoveRequest) error
	Chmod(context.Context, FileChmodRequest) error
	Touch(context.Context, FileTouchRequest) error
	UploadArchive(context.Context, FileArchiveUploadRequest, io.Reader) error
	DownloadArchive(context.Context, FileArchiveDownloadRequest, io.Writer) error
}

type fileClientFactory func(socketPath string) fileClient

type fileService struct {
	containerRoot string
	newClient     fileClientFactory
}

func NewFileService(containerRoot string) contract.FileService {
	return newFileServiceWithClientFactory(containerRoot, func(socketPath string) fileClient {
		return NewClient(socketPath)
	})
}

func newFileServiceWithClientFactory(containerRoot string, newClient fileClientFactory) contract.FileService {
	return fileService{containerRoot: containerRoot, newClient: newClient}
}

func (s fileService) client(options contract.HandlerOptions) (fileClient, error) {
	return s.clientForCapability(options, wire.CapabilityFile)
}

func (s fileService) archiveClient(options contract.HandlerOptions) (fileClient, error) {
	return s.clientForCapability(options, wire.CapabilityArchive)
}

func (s fileService) clientForCapability(options contract.HandlerOptions, capability string) (fileClient, error) {
	if options.ContainerID == "" {
		return nil, fmt.Errorf("sandboxd file service requires container id: %w", errord.ErrInvalidArgument)
	}
	if err := requireCapabilityFromLabels(options.ContainerLabels, capability); err != nil {
		return nil, err
	}
	bundlePath := filepath.Join(s.containerRoot, options.ContainerID)
	return s.newClient(runtimeoci.SandboxdBundleSocketPath(bundlePath)), nil
}

func (s fileService) StatFile(ctx context.Context, request *apipb.StatFileRequest, options contract.HandlerOptions) (*apipb.StatFileResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	response, err := client.StatFile(ctx, request.GetPath())
	if err != nil {
		return nil, fileError("stat", request.GetPath(), err)
	}
	return &apipb.StatFileResponse{Info: response.Info}, nil
}

func (s fileService) ListDir(ctx context.Context, request *apipb.ListDirRequest, options contract.HandlerOptions) (*apipb.ListDirResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	response, err := client.ListDir(ctx, request.GetPath())
	if err != nil {
		return nil, fileError("list directory", request.GetPath(), err)
	}
	return &apipb.ListDirResponse{Entries: response.Entries}, nil
}

func (s fileService) ReadFile(ctx context.Context, request *apipb.ReadFileRequest, options contract.HandlerOptions) (*apipb.ReadFileResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	response, err := client.ReadFile(ctx, request.GetPath())
	if err != nil {
		return nil, fileError("read file", request.GetPath(), err)
	}
	return &apipb.ReadFileResponse{Data: response.Data}, nil
}

func (s fileService) Exists(ctx context.Context, request *apipb.ExistsRequest, options contract.HandlerOptions) (*apipb.ExistsResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	response, err := client.Exists(ctx, request.GetPath())
	if err != nil {
		return nil, fileError("exists", request.GetPath(), err)
	}
	return &apipb.ExistsResponse{Exists: response.Exists}, nil
}

func (s fileService) WriteFile(ctx context.Context, request *apipb.WriteFileRequest, options contract.HandlerOptions) (*apipb.WriteFileResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.WriteFile(ctx, FileWriteRequest{
		Path:          request.GetPath(),
		Data:          request.GetData(),
		CreateParents: request.GetCreateParents(),
	})
	if err != nil {
		return nil, fileError("write file", request.GetPath(), err)
	}
	return &apipb.WriteFileResponse{}, nil
}

func (s fileService) Mkdir(ctx context.Context, request *apipb.MkdirRequest, options contract.HandlerOptions) (*apipb.MkdirResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.Mkdir(ctx, FileMkdirRequest{
		Path:    request.GetPath(),
		Parents: request.GetParents(),
	})
	if err != nil {
		return nil, fileError("make directory", request.GetPath(), err)
	}
	return &apipb.MkdirResponse{}, nil
}

func (s fileService) Remove(ctx context.Context, request *apipb.RemoveRequest, options contract.HandlerOptions) (*apipb.RemoveResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.Remove(ctx, FileRemoveRequest{
		Path:      request.GetPath(),
		Recursive: request.GetRecursive(),
		Force:     request.GetForce(),
	})
	if err != nil {
		return nil, fileError("remove", request.GetPath(), err)
	}
	return &apipb.RemoveResponse{}, nil
}

func (s fileService) UploadArchive(ctx context.Context, request *apipb.UploadArchiveRequest, input io.Reader, options contract.HandlerOptions) (*apipb.UploadArchiveResponse, error) {
	client, err := s.archiveClient(options)
	if err != nil {
		return nil, err
	}
	err = client.UploadArchive(ctx, FileArchiveUploadRequest{
		Path:          request.GetPath(),
		Format:        request.GetFormat(),
		CreateParents: request.GetCreateParents(),
		Overwrite:     request.GetOverwrite(),
		SymlinkPolicy: request.GetSymlinkPolicy(),
	}, input)
	if err != nil {
		return nil, archiveError("upload archive", request.GetPath(), err)
	}
	return &apipb.UploadArchiveResponse{}, nil
}

func (s fileService) DownloadArchive(ctx context.Context, request *apipb.DownloadArchiveRequest, output io.Writer, options contract.HandlerOptions) (*apipb.DownloadArchiveResponse, error) {
	client, err := s.archiveClient(options)
	if err != nil {
		return nil, err
	}
	err = client.DownloadArchive(ctx, FileArchiveDownloadRequest{
		Path:          request.GetPath(),
		Format:        request.GetFormat(),
		SymlinkPolicy: request.GetSymlinkPolicy(),
	}, output)
	if err != nil {
		return nil, archiveError("download archive", request.GetPath(), err)
	}
	return &apipb.DownloadArchiveResponse{}, nil
}

func (s fileService) Copy(ctx context.Context, request *apipb.CopyRequest, options contract.HandlerOptions) (*apipb.CopyResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.Copy(ctx, FileCopyRequest{
		SrcPath:   request.GetSrcPath(),
		DstPath:   request.GetDstPath(),
		Recursive: request.GetRecursive(),
		Overwrite: request.GetOverwrite(),
	})
	if err != nil {
		return nil, fileError("copy", request.GetSrcPath(), err)
	}
	return &apipb.CopyResponse{}, nil
}

func (s fileService) Move(ctx context.Context, request *apipb.MoveRequest, options contract.HandlerOptions) (*apipb.MoveResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.Move(ctx, FileMoveRequest{
		SrcPath:   request.GetSrcPath(),
		DstPath:   request.GetDstPath(),
		Overwrite: request.GetOverwrite(),
	})
	if err != nil {
		return nil, fileError("move", request.GetSrcPath(), err)
	}
	return &apipb.MoveResponse{}, nil
}

func (s fileService) Chmod(ctx context.Context, request *apipb.ChmodRequest, options contract.HandlerOptions) (*apipb.ChmodResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.Chmod(ctx, FileChmodRequest{
		Path:      request.GetPath(),
		Mode:      request.GetMode(),
		Recursive: request.GetRecursive(),
	})
	if err != nil {
		return nil, fileError("chmod", request.GetPath(), err)
	}
	return &apipb.ChmodResponse{}, nil
}

func (s fileService) Touch(ctx context.Context, request *apipb.TouchRequest, options contract.HandlerOptions) (*apipb.TouchResponse, error) {
	client, err := s.client(options)
	if err != nil {
		return nil, err
	}
	err = client.Touch(ctx, FileTouchRequest{
		Path:    request.GetPath(),
		Create:  request.GetCreate(),
		MtimeNs: request.GetMtimeNs(),
	})
	if err != nil {
		return nil, fileError("touch", request.GetPath(), err)
	}
	return &apipb.TouchResponse{}, nil
}

func fileError(operation string, path string, err error) error {
	return ResourceOperationError(wire.CapabilityFile, operation, path, err)
}

func archiveError(operation string, path string, err error) error {
	return ResourceOperationError(wire.CapabilityArchive, operation, path, err)
}
