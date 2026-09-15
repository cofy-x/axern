package nodev1

import (
	"context"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type NodeEnrollmentControl interface {
	Enroll(context.Context, nodekernel.EnrollmentRequest) ([]byte, error)
	Renew(context.Context, string, []byte, time.Time) ([]byte, error)
}

// EnrollmentServer belongs on the enrollment-only TLS listener, never on the
// diagnostics listener. Enrollment authenticates its token; renewal requires a
// verified Node certificate in addition to a current admission check.
type EnrollmentServer struct {
	controlnodev1.UnimplementedNodeEnrollmentServer
	Control NodeEnrollmentControl
	Cluster string
	Now     func() time.Time
}

func (s *EnrollmentServer) EnrollNode(ctx context.Context, request *controlnodev1.EnrollNodeRequest) (*controlnodev1.EnrollNodeResponse, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "TLS is required for node enrollment")
	}
	if _, ok := p.AuthInfo.(credentials.TLSInfo); !ok {
		return nil, status.Error(codes.Unauthenticated, "TLS is required for node enrollment")
	}
	certificate, err := s.Control.Enroll(ctx, nodekernel.EnrollmentRequest{NodeID: request.GetNodeID(), Token: request.GetEnrollmentToken(), CSRDER: request.GetCsrDer()})
	if err != nil {
		return nil, err
	}
	return &controlnodev1.EnrollNodeResponse{CertificatePem: certificate}, nil
}

func (s *EnrollmentServer) RenewNodeCertificate(ctx context.Context, request *controlnodev1.RenewNodeCertificateRequest) (*controlnodev1.RenewNodeCertificateResponse, error) {
	now := s.Now()
	identity, deadline, err := workloadtls.PeerIdentity(ctx, s.Cluster, now)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "verified current Node certificate is required")
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if err != nil || identity.Role != "axnoded" || identity.NodeID != request.GetNodeID() {
		return nil, status.Error(codes.PermissionDenied, "renewal requires the exact Node identity")
	}
	certificate, err := s.Control.Renew(ctx, identity.NodeID, request.GetCsrDer(), now)
	if err != nil {
		return nil, err
	}
	return &controlnodev1.RenewNodeCertificateResponse{CertificatePem: certificate}, nil
}
