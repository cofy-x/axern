package adminv1

import (
	"context"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListAdminNodes(ctx context.Context, req *adminv1.ListAdminNodesRequest) (*adminv1.ListAdminNodesResponse, error) {
	if s.deps.Nodes == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "node admin is unavailable")
	}
	lifecycle, err := adminNodeLifecycleFromProto(req.GetLifecycleStatus())
	if err != nil {
		return nil, err
	}
	records, err := s.deps.Nodes.ListNodes(ctx, adminkernel.NodeListFilter{Lifecycle: lifecycle})
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]*adminv1.AdminNode, 0, len(records))
	for _, record := range records {
		out = append(out, s.adminNodeToProto(record, now))
	}
	return &adminv1.ListAdminNodesResponse{Nodes: out}, nil
}

func (s *Server) RetireAdminNode(ctx context.Context, req *adminv1.RetireAdminNodeRequest) (*adminv1.RetireAdminNodeResponse, error) {
	if s.deps.Nodes == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "node admin is unavailable")
	}
	now := s.now()
	record, err := s.deps.Nodes.RetireNode(ctx, strings.TrimSpace(req.GetNodeID()), strings.TrimSpace(req.GetOperatorReason()), now)
	if err != nil {
		return nil, err
	}
	return &adminv1.RetireAdminNodeResponse{Node: s.adminNodeToProto(record, now)}, nil
}

func (s *Server) adminNodeToProto(record *nodekernel.Record, now time.Time) *adminv1.AdminNode {
	if record == nil {
		return &adminv1.AdminNode{}
	}
	heartbeatFresh := record.Active() && nodekernel.HeartbeatFresh(record.UpdatedAt, now, s.deps.NodeHeartbeatWindow)
	summaryFresh := record.Active() && nodekernel.SummaryFresh(record.Summary, now, s.deps.NodeSummaryWindow)
	axnoded := record.Summary.GetComponents().GetAxnoded()
	out := &adminv1.AdminNode{
		NodeID:              record.NodeID,
		LifecycleStatus:     adminNodeLifecycleToProto(record.Lifecycle),
		HeartbeatFresh:      heartbeatFresh,
		SummaryFresh:        summaryFresh,
		AxnodedReady:        heartbeatFresh && summaryFresh && axnoded.GetReady() && axnoded.GetState() == nodev1.ComponentState_COMPONENT_STATE_READY,
		HeartbeatAgeSeconds: nodekernel.HeartbeatAgeSecs(record.UpdatedAt, now),
		SummaryAgeSeconds:   nodekernel.SummaryAgeSecs(record.Summary, now),
		RegisteredAt:        timestamppb.New(record.RegisteredAt),
		UpdatedAt:           timestamppb.New(record.UpdatedAt),
		RetiredReason:       record.RetiredReason,
	}
	if !record.RetiredAt.IsZero() {
		out.RetiredAt = timestamppb.New(record.RetiredAt)
	}
	return out
}

func adminNodeLifecycleFromProto(status adminv1.AdminNodeLifecycleStatus) (nodekernel.LifecycleStatus, error) {
	switch status {
	case adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_UNSPECIFIED:
		return "", nil
	case adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE:
		return nodekernel.LifecycleActive, nil
	case adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_RETIRED:
		return nodekernel.LifecycleRetired, nil
	default:
		return "", grpcstatus.Error(codes.InvalidArgument, "invalid node lifecycle_status")
	}
}

func adminNodeLifecycleToProto(status nodekernel.LifecycleStatus) adminv1.AdminNodeLifecycleStatus {
	switch status {
	case nodekernel.LifecycleActive:
		return adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE
	case nodekernel.LifecycleRetired:
		return adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_RETIRED
	default:
		return adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_UNSPECIFIED
	}
}
