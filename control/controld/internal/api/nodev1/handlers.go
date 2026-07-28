package nodev1

import (
	"context"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	maxAllocationStatusBatch                    = 256
	allocationStatusReportStageValidateRequest  = "validate_request"
	allocationStatusReportStageAuthenticateNode = "authenticate_node"
	allocationStatusReportStagePersistStatus    = "persist_status"
)

func (s *Server) RegisterNode(ctx context.Context, req *controlnodev1.RegisterNodeRequest) (*controlnodev1.RegisterNodeResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	record, err := s.deps.NodeStore.Register(ctx, nodekernel.RegisterParams{
		NodeID:        nodeID,
		NodeTarget:    req.GetNodeTarget(),
		Runtimes:      req.GetRuntimes(),
		NodeAuthToken: req.GetNodeAuthToken(),
		Now:           s.deps.Now(),
	})
	if err != nil {
		if grpcstatus.Code(err) != codes.Unknown {
			return nil, err
		}
		return nil, grpcstatus.Errorf(codes.Internal, "persist node registration: %v", err)
	}
	s.deps.Registry.Register(record.NodeID, record.NodeTarget, record.Runtimes, record.UpdatedAt)
	return &controlnodev1.RegisterNodeResponse{}, nil
}

func (s *Server) ReportNode(ctx context.Context, req *controlnodev1.ReportNodeRequest) (*controlnodev1.ReportNodeResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeID())
	ctx, span := sdkobs.Start(ctx, ctrlobs.SpanNodeReport, attribute.String(sdkobs.AttrNodeID, nodeID))
	defer span.End()
	if nodeID == "" {
		span.SetStatus(otelcodes.Error, "node_id is required")
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if req.GetSummary() == nil {
		span.SetStatus(otelcodes.Error, "summary is required")
		return nil, grpcstatus.Error(codes.InvalidArgument, "summary is required")
	}
	if req.GetSummary().GetPools() == nil || req.GetSummary().GetPools().GetRuntimeSlots() == nil {
		span.SetStatus(otelcodes.Error, "summary.pools.runtime_slots is required")
		return nil, grpcstatus.Error(codes.InvalidArgument, "summary.pools.runtime_slots is required")
	}
	record, err := s.deps.NodeStore.Report(ctx, nodekernel.ReportParams{
		NodeID:        nodeID,
		NodeTarget:    req.GetNodeTarget(),
		Runtimes:      req.GetRuntimes(),
		Summary:       req.GetSummary(),
		NodeAuthToken: req.GetNodeAuthToken(),
		Now:           s.deps.Now(),
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(otelcodes.Error, "persist node report")
		if grpcstatus.Code(err) != codes.Unknown {
			return nil, err
		}
		return nil, grpcstatus.Errorf(codes.Internal, "persist node report: %v", err)
	}
	s.deps.Registry.Report(record.NodeID, record.NodeTarget, record.Runtimes, record.Summary, record.UpdatedAt)
	if axnodedReady(req.GetSummary()) {
		now := s.deps.Now()
		snapshot := allocationkernel.NodeInventorySnapshot{
			NodeID:              nodeID,
			ActiveAllocationIDs: req.GetSummary().GetComponents().GetAxnoded().GetActiveAllocationIds(),
			CollectedAt:         inventorySnapshotTime(req.GetSummary(), now),
		}
		if err := s.deps.Allocations.ReconcileNodeInventory(ctx, snapshot, now); err != nil {
			span.RecordError(err)
			span.SetStatus(otelcodes.Error, "reconcile node inventory")
			return nil, grpcstatus.Errorf(codes.Internal, "reconcile node inventory: %v", err)
		}
	}
	span.SetAttributes(attribute.String(sdkobs.AttrResult, "ok"))
	return &controlnodev1.ReportNodeResponse{}, nil
}

func (s *Server) BatchReportAllocationStatus(ctx context.Context, req *controlnodev1.BatchReportAllocationStatusRequest) (*controlnodev1.BatchReportAllocationStatusResponse, error) {
	stageStarted := time.Now()
	nodeID := strings.TrimSpace(req.GetNodeID())
	observations := req.GetObservations()
	ctx, op := sdkobs.StartOperation(ctx, sdkobs.OperationConfig{
		Name: ctrlobs.SpanAllocationReportStatus,
		SpanAttrs: []attribute.KeyValue{
			attribute.String(sdkobs.AttrNodeID, nodeID),
			attribute.Int("axern.batch_size", len(observations)),
		},
		Counter:  ctrlobs.MetricAllocationStatusReportTotal,
		Duration: ctrlobs.MetricAllocationStatusReportDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	if nodeID == "" {
		op.SetErrorStatus("node_id is required")
		opErr = grpcstatus.Error(codes.InvalidArgument, "node_id is required")
		recordAllocationStatusReportStage(ctx, allocationStatusReportStageValidateRequest, stageStarted, opErr)
		return nil, opErr
	}
	if err := validateAllocationStatusBatch(observations); err != nil {
		op.SetErrorStatus("invalid allocation status batch")
		opErr = err
		recordAllocationStatusReportStage(ctx, allocationStatusReportStageValidateRequest, stageStarted, err)
		return nil, opErr
	}
	recordAllocationStatusReportStage(ctx, allocationStatusReportStageValidateRequest, stageStarted, nil)
	stageStarted = time.Now()
	if err := s.deps.NodeStore.Authenticate(ctx, nodeID, req.GetNodeAuthToken()); err != nil {
		op.SetErrorStatus("authenticate node")
		opErr = err
		recordAllocationStatusReportStage(ctx, allocationStatusReportStageAuthenticateNode, stageStarted, err)
		return nil, err
	}
	recordAllocationStatusReportStage(ctx, allocationStatusReportStageAuthenticateNode, stageStarted, nil)
	stageStarted = time.Now()
	reconcileServiceIDs, err := s.deps.Allocations.BatchReportAllocationStatus(ctx, nodeID, observations, s.deps.Now())
	if err != nil {
		op.SetErrorStatus("report allocation status batch")
		opErr = err
		recordAllocationStatusReportStage(ctx, allocationStatusReportStagePersistStatus, stageStarted, err)
		return nil, err
	}
	recordAllocationStatusReportStage(ctx, allocationStatusReportStagePersistStatus, stageStarted, nil)
	if s.deps.NotifyServiceReconcile != nil && len(reconcileServiceIDs) > 0 {
		s.deps.NotifyServiceReconcile(reconcileServiceIDs...)
	}
	return &controlnodev1.BatchReportAllocationStatusResponse{}, nil
}

func validateAllocationStatusBatch(observations []*controlnodev1.AllocationStatusObservation) error {
	if len(observations) == 0 {
		return grpcstatus.Error(codes.InvalidArgument, "at least one allocation status observation is required")
	}
	if len(observations) > maxAllocationStatusBatch {
		return grpcstatus.Errorf(codes.InvalidArgument, "allocation status batch exceeds limit %d", maxAllocationStatusBatch)
	}
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		allocationID := strings.TrimSpace(observation.GetAllocationID())
		if allocationID == "" || observation.GetAttempt() <= 0 || !allocationStatusValid(observation.GetStatus()) {
			return grpcstatus.Error(codes.InvalidArgument, "allocation status observation requires allocation_id, attempt, and status")
		}
		if _, ok := seen[allocationID]; ok {
			return grpcstatus.Errorf(codes.InvalidArgument, "duplicate allocation status observation %q", allocationID)
		}
		seen[allocationID] = struct{}{}
	}
	return nil
}

func allocationStatusValid(status commonv1.AllocationStatus) bool {
	if status == commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED {
		return false
	}
	_, known := commonv1.AllocationStatus_name[int32(status)]
	return known
}

func recordAllocationStatusReportStage(ctx context.Context, stage string, started time.Time, err error) {
	if started.IsZero() {
		return
	}
	result := sdkobs.ResultOK
	errorClass := ""
	if err != nil {
		result = sdkobs.ResultError
		errorClass = allocationReportErrorClass(err)
	}
	sdkobs.DurationHistogram(ctrlobs.MetricAllocationStatusReportStageDuration.Name, ctrlobs.MetricAllocationStatusReportStageDuration.Description).RecordDuration(ctx, time.Since(started),
		attribute.String(sdkobs.AttrStage, stage),
		attribute.String(sdkobs.AttrResult, result),
		attribute.String(sdkobs.AttrErrorClass, errorClass),
	)
}

func allocationReportErrorClass(err error) string {
	if err == nil {
		return ""
	}
	code := grpcstatus.Code(err)
	if code != codes.OK && code != codes.Unknown {
		return strings.ToLower(code.String())
	}
	return "error"
}

func (s *Server) WatchExecutionLeases(req *controlnodev1.WatchExecutionLeasesRequest, stream controlnodev1.NodeControl_WatchExecutionLeasesServer) error {
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := s.deps.NodeStore.Authenticate(stream.Context(), nodeID, req.GetNodeAuthToken()); err != nil {
		return err
	}
	revision := req.GetAfterRevision()
	for {
		leases, current, err := s.deps.Allocations.WatchExecutionLeases(stream.Context(), nodeID, revision, s.deps.Now())
		if err != nil {
			return err
		}
		if current <= revision {
			return grpcstatus.Error(codes.Internal, "execution lease watch returned a non-advancing revision")
		}
		if err := stream.Send(&controlnodev1.WatchExecutionLeasesResponse{Leases: leases, CurrentRevision: current}); err != nil {
			return err
		}
		revision = current
	}
}

func (s *Server) WatchTunnelSessions(req *controlnodev1.WatchTunnelSessionsRequest, stream controlnodev1.NodeControl_WatchTunnelSessionsServer) error {
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if s.deps.Tunnels == nil {
		return grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
	}
	if err := s.deps.NodeStore.Authenticate(stream.Context(), nodeID, req.GetNodeAuthToken()); err != nil {
		return err
	}
	sessions, revision, err := s.deps.Tunnels.WatchNode(stream.Context(), nodeID, req.GetAfterRevision(), s.deps.Now())
	if err != nil {
		return err
	}
	return stream.Send(&controlnodev1.WatchTunnelSessionsResponse{Sessions: sessions, CurrentRevision: revision})
}

func (s *Server) ReportTunnelSessionStatus(ctx context.Context, req *controlnodev1.ReportTunnelSessionStatusRequest) (*controlnodev1.ReportTunnelSessionStatusResponse, error) {
	if strings.TrimSpace(req.GetSessionID()) == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "session_id is required")
	}
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if s.deps.Tunnels == nil {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "tunnel control is not configured")
	}
	if err := s.deps.NodeStore.Authenticate(ctx, nodeID, req.GetNodeAuthToken()); err != nil {
		return nil, err
	}
	if _, err := s.deps.Tunnels.ReportStatus(ctx, nodeID, req.GetSessionID(), req.GetStatus(), req.GetReason(), req.GetBoundAddr(), s.deps.Now()); err != nil {
		return nil, err
	}
	return &controlnodev1.ReportTunnelSessionStatusResponse{}, nil
}

func axnodedReady(summary *controlnodev1.NodeSummary) bool {
	if summary == nil || summary.GetComponents() == nil || summary.GetComponents().GetAxnoded() == nil {
		return false
	}
	axnoded := summary.GetComponents().GetAxnoded()
	return axnoded.GetReady() && axnoded.GetState() == controlnodev1.ComponentState_COMPONENT_STATE_READY
}

func inventorySnapshotTime(summary *controlnodev1.NodeSummary, fallback time.Time) time.Time {
	if summary == nil || summary.GetCollectedAt() == nil {
		return fallback
	}
	collectedAt := summary.GetCollectedAt().AsTime()
	if collectedAt.IsZero() {
		return fallback
	}
	return collectedAt
}
