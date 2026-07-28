package adminkernel

import (
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

type NodeListFilter struct {
	Lifecycle nodekernel.LifecycleStatus
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
