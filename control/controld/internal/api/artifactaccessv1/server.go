package artifactaccessv1

import (
	"context"
	"crypto/x509"
	"time"

	rolloutkernel "github.com/cofy-x/axern/control/controld/internal/kernel/rollout"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	artifactv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/rollout/artifact/v1"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	artifactv1.UnimplementedArtifactAccessServer
	reader rolloutkernel.ArtifactReader
}

func New(reader rolloutkernel.ArtifactReader) *Server { return &Server{reader: reader} }
func (s *Server) ResolveDownloadTicket(ctx context.Context, req *artifactv1.ResolveDownloadTicketRequest) (*artifactv1.ResolveDownloadTicketResponse, error) {
	result := "error"
	defer func() {
		sdkobs.Int64Counter(ctrlobs.MetricArtifactTicketTotal.Name, ctrlobs.MetricArtifactTicketTotal.Description).Add(ctx, 1,
			attribute.String(sdkobs.AttrOperation, "resolve"), attribute.String(sdkobs.AttrResult, result))
	}()
	if err := authorizeGateway(ctx); err != nil {
		return nil, err
	}
	artifact, url, headers, expires, err := s.reader.ResolveArtifactDownload(ctx, req.GetTicket(), req.GetOffset(), 5*time.Minute)
	if err != nil {
		return nil, err
	}
	result = "ok"
	return &artifactv1.ResolveDownloadTicketResponse{Artifact: artifact, Url: url, Headers: headers, ExpiresAt: timestamppb.New(expires)}, nil
}

func authorizeGateway(ctx context.Context) error {
	connection, ok := peer.FromContext(ctx)
	if !ok {
		return status.Error(codes.PermissionDenied, "artifact ticket resolution requires gateway identity")
	}
	tlsInfo, ok := connection.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) == 0 {
		return status.Error(codes.PermissionDenied, "artifact ticket resolution requires verified gateway mTLS")
	}
	if !certificateHasIdentity(tlsInfo.State.PeerCertificates[0], "gatewayd") {
		return status.Error(codes.PermissionDenied, "artifact ticket resolution is restricted to gatewayd")
	}
	return nil
}

func certificateHasIdentity(certificate *x509.Certificate, identity string) bool {
	if certificate == nil {
		return false
	}
	return certificate.Subject.CommonName == identity
}
