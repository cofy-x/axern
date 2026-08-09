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

func (s *Server) GetNodeCapabilitySnapshot(ctx context.Context, req *adminv1.GetNodeCapabilitySnapshotRequest) (*adminv1.GetNodeCapabilitySnapshotResponse, error) {
	if s.deps.CapabilityDiagnostics == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "capability diagnostics are unavailable")
	}
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	snapshot, err := s.deps.CapabilityDiagnostics.GetNodeCapabilitySnapshot(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	return &adminv1.GetNodeCapabilitySnapshotResponse{Snapshot: snapshot}, nil
}

func (s *Server) ListNodeCapabilityTransitions(ctx context.Context, req *adminv1.ListNodeCapabilityTransitionsRequest) (*adminv1.ListNodeCapabilityTransitionsResponse, error) {
	if s.deps.CapabilityDiagnostics == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "capability diagnostics are unavailable")
	}
	items, err := s.deps.CapabilityDiagnostics.ListNodeCapabilityTransitions(ctx, strings.TrimSpace(req.GetNodeID()), req.GetLimit())
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.AdminCapabilityTransition, 0, len(items))
	for _, item := range items {
		out = append(out, &adminv1.AdminCapabilityTransition{
			TransitionID: item.TransitionID, NodeID: item.NodeID, SnapshotID: item.SnapshotID,
			SnapshotSequence: item.SnapshotSequence, Key: item.Key, OldState: item.OldState,
			NewState: item.NewState, OldEvidenceID: item.OldEvidenceID, NewEvidenceID: item.NewEvidenceID,
			ReasonCode: item.ReasonCode, Reason: item.Reason, ObservedAt: timestamppb.New(item.ObservedAt),
			ReportedAt: timestamppb.New(item.ReportedAt),
		})
	}
	return &adminv1.ListNodeCapabilityTransitionsResponse{Transitions: out}, nil
}

func (s *Server) ListCapabilityReconcileQueue(ctx context.Context, req *adminv1.ListCapabilityReconcileQueueRequest) (*adminv1.ListCapabilityReconcileQueueResponse, error) {
	if s.deps.CapabilityDiagnostics == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "capability diagnostics are unavailable")
	}
	items, err := s.deps.CapabilityDiagnostics.ListCapabilityReconcileQueue(ctx, strings.TrimSpace(req.GetNodeID()), req.GetLimit())
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.AdminCapabilityReconcileItem, 0, len(items))
	for i := range items {
		out = append(out, capabilityReconcileItemToProto(&items[i]))
	}
	return &adminv1.ListCapabilityReconcileQueueResponse{Items: out}, nil
}

func (s *Server) GetAllocationCapabilityDiagnostics(ctx context.Context, req *adminv1.GetAllocationCapabilityDiagnosticsRequest) (*adminv1.GetAllocationCapabilityDiagnosticsResponse, error) {
	if s.deps.CapabilityDiagnostics == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "capability diagnostics are unavailable")
	}
	allocationID := strings.TrimSpace(req.GetAllocationID())
	if allocationID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	diagnostics, err := s.deps.CapabilityDiagnostics.GetAllocationCapabilityDiagnostics(ctx, allocationID)
	if err != nil {
		return nil, err
	}
	return &adminv1.GetAllocationCapabilityDiagnosticsResponse{
		AllocationID: diagnostics.AllocationID, NodeID: diagnostics.NodeID,
		RequiredDependencies: diagnostics.Dependencies, AdmittedDependencies: diagnostics.AdmittedDependencies,
		Conditions: diagnostics.Conditions, Reconcile: capabilityReconcileItemToProto(diagnostics.Reconcile),
	}, nil
}

func capabilityReconcileItemToProto(item *adminkernel.CapabilityReconcileItem) *adminv1.AdminCapabilityReconcileItem {
	if item == nil {
		return nil
	}
	out := &adminv1.AdminCapabilityReconcileItem{
		AllocationID: item.AllocationID, NodeID: item.NodeID, PendingDependencies: item.Dependencies,
		Attempts: item.Attempts, NextRunAt: timestamppb.New(item.NextRunAt), LastError: item.LastError,
		UpdatedAt: timestamppb.New(item.UpdatedAt),
	}
	if item.LeaseExpiresAt != nil {
		out.LeaseExpiresAt = timestamppb.New(*item.LeaseExpiresAt)
	}
	return out
}

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
