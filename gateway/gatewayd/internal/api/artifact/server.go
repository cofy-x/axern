package artifact

import (
	"errors"

	artifactapp "github.com/cofy-x/axern/gateway/gatewayd/internal/application/artifact"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/data/artifact/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	artifactv1.UnimplementedArtifactDataServer
	service *artifactapp.Service
}

func New(service *artifactapp.Service) *Server { return &Server{service: service} }
func (s *Server) DownloadArtifact(req *artifactv1.DownloadArtifactRequest, stream artifactv1.ArtifactData_DownloadArtifactServer) error {
	err := s.service.Download(stream.Context(), req.GetTicket(), req.GetOffset(), func(offset int64, data []byte) error {
		return stream.Send(&artifactv1.DownloadArtifactResponse{Offset: offset, Data: data})
	})
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, artifactapp.ErrConcurrency), errors.Is(err, artifactapp.ErrTooLarge):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, artifactapp.ErrRange):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, artifactapp.ErrExcess), errors.Is(err, artifactapp.ErrTruncated), errors.Is(err, artifactapp.ErrDigest):
		return status.Error(codes.DataLoss, err.Error())
	case errors.Is(err, artifactapp.ErrUpstream):
		return status.Error(codes.Unavailable, err.Error())
	default:
		return err
	}
}
