package appnode

import (
	"context"
	"crypto/x509"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Enrollment struct {
	Store  nodekernel.EnrollmentStore
	Issuer workloadtls.FileIssuer
}

func (e *Enrollment) Enroll(ctx context.Context, request nodekernel.EnrollmentRequest) ([]byte, error) {
	if request.NodeID == "" || strings.TrimSpace(request.NodeID) != request.NodeID || len(request.Token) < 32 {
		return nil, status.Error(codes.InvalidArgument, "node identity and enrollment token are required")
	}
	if len(request.CSRDER) == 0 || len(request.CSRDER) > 16<<10 {
		return nil, status.Error(codes.InvalidArgument, "invalid enrollment CSR size")
	}
	csr, err := x509.ParseCertificateRequest(request.CSRDER)
	if err != nil || csr.CheckSignature() != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid enrollment CSR signature")
	}
	if _, err := (workloadtls.Identity{Cluster: e.Issuer.Cluster, Role: "axnoded", NodeID: request.NodeID}).URI(); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid enrollment Node identity")
	}
	return e.Store.Enroll(ctx, request, e)
}

func (e *Enrollment) SignNode(csrDER []byte, nodeID string, now time.Time) ([]byte, error) {
	return e.Issuer.Sign(csrDER, workloadtls.Identity{Cluster: e.Issuer.Cluster, Role: "axnoded", NodeID: nodeID}, now)
}

// Renew accepts only the Node identity authenticated by the API transport.
// Renewal never reads an enrollment token and never extends its replay window.
func (e *Enrollment) Renew(ctx context.Context, nodeID string, csrDER []byte, now time.Time) ([]byte, error) {
	if nodeID == "" || now.IsZero() || len(csrDER) == 0 || len(csrDER) > 16<<10 {
		return nil, status.Error(codes.InvalidArgument, "invalid node certificate renewal")
	}
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil || csr.CheckSignature() != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid renewal CSR signature")
	}
	return e.Store.RenewCertificate(ctx, nodeID, csrDER, now, e)
}
