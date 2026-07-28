package api

import (
	"io"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	filev1 "github.com/cofy-x/axern/sdk/go/gen/axern/common/file/v1"
	nodesandboxv1 "github.com/cofy-x/axern/sdk/go/gen/axern/node/sandbox/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *nodeSandboxServer) UploadArchive(stream nodesandboxv1.NodeSandbox_UploadArchiveServer) error {
	first, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			return grpcstatus.Error(codes.InvalidArgument, "initial open payload is required")
		}
		return err
	}
	open := first.GetOpen()
	if open == nil {
		return grpcstatus.Error(codes.InvalidArgument, "initial open payload is required")
	}
	target, err := s.validateDirectAuth(stream.Context(), open.GetAllocationID(), open.GetAttempt(), open.GetExecutionLeaseToken())
	if err != nil {
		return err
	}
	if err := validateArchiveRequest(open.GetPath(), open.GetFormat(), open.GetSymlinkPolicy()); err != nil {
		return err
	}
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}
	_, err = s.svc.UploadArchive(stream.Context(), &runtimev1.UploadArchiveRequest{
		ID:            target.targetID,
		Path:          open.GetPath(),
		Format:        open.GetFormat(),
		CreateParents: open.GetCreateParents(),
		Overwrite:     open.GetOverwrite(),
		SymlinkPolicy: open.GetSymlinkPolicy(),
	}, &uploadArchiveReader{stream: stream})
	if err != nil {
		return err
	}
	return stream.SendAndClose(&nodesandboxv1.UploadArchiveResponse{})
}

func (s *nodeSandboxServer) DownloadArchive(req *nodesandboxv1.DownloadArchiveRequest, stream nodesandboxv1.NodeSandbox_DownloadArchiveServer) error {
	target, err := s.validateDirectAuth(stream.Context(), req.GetAllocationID(), req.GetAttempt(), req.GetExecutionLeaseToken())
	if err != nil {
		return err
	}
	if err := validateArchiveRequest(req.GetPath(), req.GetFormat(), req.GetSymlinkPolicy()); err != nil {
		return err
	}
	if err := acknowledgeExecutionLease(stream); err != nil {
		return err
	}
	_, err = s.svc.DownloadArchive(stream.Context(), &runtimev1.DownloadArchiveRequest{
		ID:            target.targetID,
		Path:          req.GetPath(),
		Format:        req.GetFormat(),
		SymlinkPolicy: req.GetSymlinkPolicy(),
	}, downloadArchiveWriter{stream: stream})
	return err
}

func validateArchiveRequest(path string, format filev1.SandboxArchiveFormat, policy filev1.SandboxArchiveSymlinkPolicy) error {
	if path == "" {
		return grpcstatus.Error(codes.InvalidArgument, "path is required")
	}
	if format != filev1.SandboxArchiveFormat_SANDBOX_ARCHIVE_FORMAT_TAR {
		return grpcstatus.Error(codes.InvalidArgument, "only tar archives are supported")
	}
	if policy != filev1.SandboxArchiveSymlinkPolicy_SANDBOX_ARCHIVE_SYMLINK_POLICY_REJECT {
		return grpcstatus.Error(codes.InvalidArgument, "only reject symlink policy is supported")
	}
	return nil
}

type uploadArchiveReader struct {
	stream nodesandboxv1.NodeSandbox_UploadArchiveServer
	buf    []byte
}

func (r *uploadArchiveReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		msg, err := r.stream.Recv()
		if err != nil {
			if err == io.EOF {
				return 0, io.EOF
			}
			return 0, err
		}
		payload, ok := msg.GetPayload().(*nodesandboxv1.UploadArchiveRequest_Chunk)
		if !ok {
			return 0, grpcstatus.Error(codes.InvalidArgument, "archive stream payload must be chunk after open")
		}
		r.buf = payload.Chunk
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

type downloadArchiveWriter struct {
	stream nodesandboxv1.NodeSandbox_DownloadArchiveServer
}

func (w downloadArchiveWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	chunk := append([]byte(nil), p...)
	if err := w.stream.Send(&nodesandboxv1.DownloadArchiveResponse{Chunk: chunk}); err != nil {
		return 0, err
	}
	return len(p), nil
}

var _ io.Reader = (*uploadArchiveReader)(nil)
var _ io.Writer = downloadArchiveWriter{}
