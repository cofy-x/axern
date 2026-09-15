package adminkernel

import (
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/lib/go/grpcclient/workloadtls"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type NodeListFilter struct {
	Lifecycle nodekernel.LifecycleStatus
}

type AdmitNodeRequest struct {
	NodeID          string
	EnrollmentToken string
	OperatorReason  string
	Now             time.Time
}

func NormalizeAdmitNodeRequest(in AdmitNodeRequest) AdmitNodeRequest {
	return AdmitNodeRequest{
		NodeID:          strings.TrimSpace(in.NodeID),
		EnrollmentToken: strings.TrimSpace(in.EnrollmentToken),
		OperatorReason:  strings.TrimSpace(in.OperatorReason),
		Now:             in.Now.UTC(),
	}
}

func ValidateAdmitNodeRequest(req AdmitNodeRequest) error {
	if err := workloadtls.ValidateNodeID(req.NodeID); err != nil {
		return grpcstatus.Error(codes.InvalidArgument, err.Error())
	}
	if req.EnrollmentToken == "" {
		return grpcstatus.Error(codes.InvalidArgument, "enrollment_token is required")
	}
	if len(req.EnrollmentToken) < 32 {
		return grpcstatus.Error(codes.InvalidArgument, "enrollment_token must contain at least 32 characters")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	if req.Now.IsZero() {
		return grpcstatus.Error(codes.InvalidArgument, "admission time is required")
	}
	return nil
}

type RetireNodeRequest struct {
	NodeID          string
	OperatorReason  string
	Now             time.Time
	HeartbeatWindow time.Duration
}

func NormalizeRetireNodeRequest(in RetireNodeRequest) RetireNodeRequest {
	return RetireNodeRequest{
		NodeID:          strings.TrimSpace(in.NodeID),
		OperatorReason:  strings.TrimSpace(in.OperatorReason),
		Now:             in.Now.UTC(),
		HeartbeatWindow: in.HeartbeatWindow,
	}
}

func ValidateRetireNodeRequest(req RetireNodeRequest) error {
	if req.NodeID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	if req.Now.IsZero() {
		return grpcstatus.Error(codes.InvalidArgument, "retirement time is required")
	}
	if req.HeartbeatWindow <= 0 {
		return grpcstatus.Error(codes.InvalidArgument, "heartbeat freshness window is required")
	}
	return nil
}

type RevokeNodeRequest struct {
	NodeID         string
	OperatorReason string
	Now            time.Time
}

func NormalizeRevokeNodeRequest(in RevokeNodeRequest) RevokeNodeRequest {
	return RevokeNodeRequest{NodeID: strings.TrimSpace(in.NodeID), OperatorReason: strings.TrimSpace(in.OperatorReason), Now: in.Now.UTC()}
}

func ValidateRevokeNodeRequest(req RevokeNodeRequest) error {
	if req.NodeID == "" || req.OperatorReason == "" || req.Now.IsZero() {
		return grpcstatus.Error(codes.InvalidArgument, "node_id, operator_reason and revocation time are required")
	}
	return nil
}
