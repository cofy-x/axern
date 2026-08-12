package nodev1

import (
	"context"
	"math"
	"strings"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	ctrlobs "github.com/cofy-x/axern/control/controld/internal/observability"
	"github.com/cofy-x/axern/lib/go/memorybudget"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
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
	if err := validateNodeMemoryBudget(req.GetSummary(), s.deps.Now()); err != nil {
		span.SetStatus(otelcodes.Error, "invalid node memory budget")
		return nil, grpcstatus.Errorf(codes.InvalidArgument, "summary.memory_budget: %v", err)
	}
	if s.deps.Reporter == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "node reporter is unavailable")
	}
	err := s.deps.Reporter.Report(ctx, nodekernel.ReportParams{
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
	span.SetAttributes(attribute.String(sdkobs.AttrResult, "ok"))
	return &controlnodev1.ReportNodeResponse{}, nil
}

func validateNodeMemoryBudget(summary *controlnodev1.NodeSummary, now time.Time) error {
	budget := summary.GetMemoryBudget()
	if !memorybudget.Published(budget) {
		return nil
	}
	return memorybudget.ValidateSummary(summary, now)
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
		if _, ok := commonv1.WorkloadDiagnosticCode_name[int32(observation.GetDiagnosticCode())]; !ok {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation status observation %q has invalid diagnostic_code", allocationID)
		}
		if _, ok := seen[allocationID]; ok {
			return grpcstatus.Errorf(codes.InvalidArgument, "duplicate allocation status observation %q", allocationID)
		}
		seen[allocationID] = struct{}{}
	}
	return nil
}

func (s *Server) BatchReportAllocationCapabilityConditions(ctx context.Context, req *controlnodev1.BatchReportAllocationCapabilityConditionsRequest) (*controlnodev1.BatchReportAllocationCapabilityConditionsResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAllocationCapabilityConditionBatch(req.GetReports(), s.deps.Now()); err != nil {
		return nil, err
	}
	if err := s.deps.NodeStore.Authenticate(ctx, nodeID, req.GetNodeAuthToken()); err != nil {
		return nil, err
	}
	if err := s.deps.Allocations.BatchReportAllocationCapabilityConditions(ctx, nodeID, req.GetReports(), s.deps.Now()); err != nil {
		return nil, err
	}
	return &controlnodev1.BatchReportAllocationCapabilityConditionsResponse{}, nil
}

func validateAllocationCapabilityConditionBatch(reports []*controlnodev1.AllocationCapabilityConditionReport, now time.Time) error {
	if len(reports) == 0 {
		return grpcstatus.Error(codes.InvalidArgument, "at least one allocation capability condition report is required")
	}
	if len(reports) > maxAllocationStatusBatch {
		return grpcstatus.Errorf(codes.InvalidArgument, "allocation capability condition batch exceeds limit %d", maxAllocationStatusBatch)
	}
	seen := make(map[string]struct{}, len(reports))
	for _, report := range reports {
		allocationID := strings.TrimSpace(report.GetAllocationID())
		if allocationID == "" || report.GetAttempt() <= 0 {
			return grpcstatus.Error(codes.InvalidArgument, "allocation capability condition report requires allocation_id and attempt")
		}
		if _, duplicate := seen[allocationID]; duplicate {
			return grpcstatus.Errorf(codes.InvalidArgument, "duplicate allocation capability condition report %q", allocationID)
		}
		seen[allocationID] = struct{}{}
		if err := capabilitycontract.ValidateConditionSet(report.GetConditionSet(), now); err != nil {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q capability conditions: %v", allocationID, err)
		}
	}
	return nil
}

func (s *Server) BatchReportAllocationMemoryObservations(ctx context.Context, req *controlnodev1.BatchReportAllocationMemoryObservationsRequest) (*controlnodev1.BatchReportAllocationMemoryObservationsResponse, error) {
	nodeID := strings.TrimSpace(req.GetNodeID())
	if nodeID == "" {
		return nil, grpcstatus.Error(codes.InvalidArgument, "node_id is required")
	}
	if err := validateAllocationMemoryObservationBatch(req.GetObservations(), s.deps.Now()); err != nil {
		return nil, err
	}
	if err := s.deps.NodeStore.Authenticate(ctx, nodeID, req.GetNodeAuthToken()); err != nil {
		return nil, err
	}
	if err := s.deps.Allocations.BatchReportAllocationMemoryObservations(ctx, nodeID, req.GetObservations(), s.deps.Now()); err != nil {
		return nil, err
	}
	return &controlnodev1.BatchReportAllocationMemoryObservationsResponse{}, nil
}

func validateAllocationMemoryObservationBatch(observations []*controlnodev1.AllocationMemoryObservation, now time.Time) error {
	if len(observations) == 0 {
		return grpcstatus.Error(codes.InvalidArgument, "at least one allocation memory observation is required")
	}
	if len(observations) > maxAllocationStatusBatch {
		return grpcstatus.Errorf(codes.InvalidArgument, "allocation memory observation batch exceeds limit %d", maxAllocationStatusBatch)
	}
	seen := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		allocationID := strings.TrimSpace(observation.GetAllocationID())
		if allocationID == "" || observation.GetAttempt() <= 0 || observation.GetRevision() <= 0 || observation.GetObservedAt() == nil {
			return grpcstatus.Error(codes.InvalidArgument, "allocation memory observation requires allocation_id, attempt, revision, and observed_at")
		}
		if err := observation.GetObservedAt().CheckValid(); err != nil || observation.GetObservedAt().AsTime().After(now.Add(time.Minute)) {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q has invalid observed_at", allocationID)
		}
		if _, duplicate := seen[allocationID]; duplicate {
			return grpcstatus.Errorf(codes.InvalidArgument, "duplicate allocation memory observation %q", allocationID)
		}
		seen[allocationID] = struct{}{}
		values := []int64{
			observation.GetRequestBytes(), observation.GetLimitBytes(), observation.GetCurrentBytes(), observation.GetPeakBytes(), observation.GetSwapCurrentBytes(),
			observation.GetAnonBytes(), observation.GetFileBytes(), observation.GetShmemBytes(), observation.GetKernelBytes(), observation.GetDirtyBytes(), observation.GetWritebackBytes(),
		}
		for _, value := range values {
			if value < 0 {
				return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q memory values must be non-negative", allocationID)
			}
		}
		bounded := observation.GetLimitBytes() > 0
		if bounded && observation.GetRequestBytes() > observation.GetLimitBytes() {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q has inconsistent request and limit", allocationID)
		}
		if observation.GetPeakBytes() < observation.GetCurrentBytes() || (bounded && observation.GetSwapCurrentBytes() != 0) {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q has inconsistent memory peak or nonzero swap", allocationID)
		}
		if !observation.GetPeakAvailable() && observation.GetPeakBytes() != observation.GetCurrentBytes() {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q reports a non-kernel peak that differs from its current sample", allocationID)
		}
		cleanupState := observation.GetCleanupState()
		switch cleanupState {
		case controlnodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_ASSIGNED:
			if observation.GetParentControlsVerified() != bounded || observation.GetLeafControlsVerified() != bounded {
				return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q has inconsistent assigned hard-limit verification", allocationID)
			}
		case controlnodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_RETIRING:
			// Runtime deletion may remove the workload leaf before parent memcg
			// page cache and writeback have converged. The allocation parent is
			// still the cleanup-debt boundary; a missing leaf is not evidence loss.
			if observation.GetParentControlsVerified() != bounded || (!bounded && observation.GetLeafControlsVerified()) || observation.GetPidRolesVerified() {
				return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q has inconsistent retiring memory verification", allocationID)
			}
		default:
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q memory cleanup state is invalid", allocationID)
		}
		if observation.GetPsiSomeAvg10() < 0 || observation.GetPsiFullAvg10() < 0 ||
			math.IsNaN(observation.GetPsiSomeAvg10()) || math.IsNaN(observation.GetPsiFullAvg10()) ||
			math.IsInf(observation.GetPsiSomeAvg10(), 0) || math.IsInf(observation.GetPsiFullAvg10(), 0) {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q has invalid memory pressure values", allocationID)
		}
		if !observation.GetPsiAvailable() && (observation.GetPsiSomeAvg10() != 0 || observation.GetPsiFullAvg10() != 0 ||
			observation.GetPsiSomeTotalUsec() != 0 || observation.GetPsiFullTotalUsec() != 0) {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q reports memory pressure values without PSI availability", allocationID)
		}
		runtimeName := strings.TrimSpace(observation.GetRuntime())
		if strings.TrimSpace(observation.GetCgroupIdentity()) == "" || (runtimeName != "runc" && runtimeName != "runsc") ||
			len(observation.GetCgroupIdentity()) > 1024 || len(observation.GetRuntime()) > 64 {
			return grpcstatus.Errorf(codes.InvalidArgument, "allocation %q memory identity or cleanup state is invalid", allocationID)
		}
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
