package adminv1

import (
	"context"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) CheckConsistency(ctx context.Context, _ *adminv1.CheckConsistencyRequest) (*adminv1.CheckConsistencyResponse, error) {
	if s.deps.Reliability == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "admin reliability is unavailable")
	}
	snapshot, err := s.deps.Reliability.ConsistencySnapshot(ctx, s.now())
	if err != nil {
		return nil, err
	}
	return &adminv1.CheckConsistencyResponse{Snapshot: consistencySnapshotToProto(snapshot)}, nil
}

func (s *Server) GetAdminReliabilityHealth(ctx context.Context, _ *adminv1.GetAdminReliabilityHealthRequest) (*adminv1.GetAdminReliabilityHealthResponse, error) {
	if s.deps.Reliability == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "admin reliability is unavailable")
	}
	health, err := s.deps.Reliability.Health(ctx, s.now())
	if err != nil {
		return nil, err
	}
	return &adminv1.GetAdminReliabilityHealthResponse{Health: reliabilityHealthToProto(health)}, nil
}

func reliabilityHealthToProto(health adminkernel.ReliabilityHealth) *adminv1.AdminReliabilityHealth {
	signals := make([]*adminv1.AdminReliabilitySignal, 0, len(health.Signals))
	for _, signal := range health.Signals {
		signals = append(signals, &adminv1.AdminReliabilitySignal{
			Code:    reliabilitySignalCodeToProto(signal.Code),
			Message: signal.Message,
		})
	}
	components := make([]*adminv1.ReconcileComponentHealth, 0, len(health.ReconcileComponents))
	for _, component := range health.ReconcileComponents {
		components = append(components, &adminv1.ReconcileComponentHealth{
			Component:           component.Component,
			Running:             component.Running,
			LastStartedAt:       optionalTimestamp(component.LastStartedAt),
			LastSuccessAt:       optionalTimestamp(component.LastSuccessAt),
			LastErrorAt:         optionalTimestamp(component.LastErrorAt),
			LastError:           component.LastError,
			ConsecutiveFailures: component.ConsecutiveFailures,
		})
	}
	return &adminv1.AdminReliabilityHealth{
		Status:                        reliabilityStatusToProto(health.Status),
		Consistency:                   consistencySnapshotToProto(health.Consistency),
		AllocationLifecycleRetries:    health.AllocationLifecycleRetries,
		DueAllocationLifecycleRetries: health.DueAllocationLifecycleRetries,
		ReconcileUnhealthyComponents:  health.ReconcileUnhealthyComponents,
		StorageBindingHealth:          storageBindingHealthToProto(health.StorageBindingHealth),
		NodeVolumeHealth:              nodeVolumeHealthToProto(health.NodeVolumeHealth),
		NodeFleetHealth:               nodeFleetHealthToProto(health.NodeFleetHealth),
		Signals:                       signals,
		ReconcileComponents:           components,
	}
}

func nodeFleetHealthToProto(health adminkernel.NodeFleetHealth) *adminv1.AdminNodeFleetHealth {
	return &adminv1.AdminNodeFleetHealth{
		Unavailable:         health.Unavailable,
		Error:               health.Error,
		ActiveNodes:         health.ActiveNodes,
		ReadyNodes:          health.ReadyNodes,
		StaleHeartbeatNodes: health.StaleHeartbeatNodes,
		StaleSummaryNodes:   health.StaleSummaryNodes,
		NotReadyNodes:       health.NotReadyNodes,
	}
}

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

func storageBindingHealthToProto(health adminkernel.StorageBindingHealth) *adminv1.AdminStorageBindingHealth {
	return &adminv1.AdminStorageBindingHealth{
		Unavailable:            health.Unavailable,
		Error:                  health.Error,
		FailedBindings:         health.FailedBindings,
		ReleasingBindings:      health.ReleasingBindings,
		StuckReleasingBindings: health.StuckReleasingBindings,
		InconsistentClaims:     health.InconsistentClaims,
		InvalidBindings:        health.InvalidBindings,
		DeletingClaims:         health.DeletingClaims,
		StuckDeletingClaims:    health.StuckDeletingClaims,
	}
}

func nodeVolumeHealthToProto(health adminkernel.NodeVolumeHealth) *adminv1.AdminNodeVolumeHealth {
	return &adminv1.AdminNodeVolumeHealth{
		UnhealthyNodes:                health.UnhealthyNodes,
		PublishedVolumes:              health.PublishedVolumes,
		LastReconcileStaleAllocations: health.LastReconcileStaleAllocations,
		LastReconcileInvalidVolumes:   health.LastReconcileInvalidVolumes,
		Error:                         health.Error,
	}
}

func consistencySnapshotToProto(snapshot consistencykernel.Snapshot) *adminv1.ConsistencySnapshot {
	issues := make([]*adminv1.ConsistencyIssue, 0, len(snapshot.Issues))
	for _, issue := range snapshot.Issues {
		repair := consistencykernel.RepairPlanForIssue(issue)
		issues = append(issues, &adminv1.ConsistencyIssue{
			Code:             consistencyIssueCodeToProto(issue.Code),
			Severity:         consistencyIssueSeverityToProto(issue.Severity),
			AllocationID:     issue.AllocationID,
			OwnerType:        issue.OwnerType,
			OwnerID:          issue.OwnerID,
			NodeID:           issue.NodeID,
			Status:           issue.Status,
			Detail:           issue.Detail,
			RepairOwner:      consistencyRepairOwnerToProto(repair.Owner),
			RepairAction:     consistencyRepairActionToProto(repair.Action),
			AutomaticRepair:  repair.Automatic,
			RepairTargetType: consistencyRepairTargetTypeToProto(repair.TargetType),
			RepairTargetID:   repair.TargetID,
		})
	}
	return &adminv1.ConsistencySnapshot{
		Status: consistencyStatusToProto(snapshot.Status),
		Counts: &adminv1.ConsistencyCounts{
			ActiveReservations:         snapshot.Counts.ActiveReservations,
			ActiveLeases:               snapshot.Counts.ActiveLeases,
			ActiveTunnels:              snapshot.Counts.ActiveTunnels,
			AllocationLifecycleRetries: snapshot.Counts.ReconcileQueue,
			Issues:                     snapshot.Counts.Issues,
		},
		Issues:    issues,
		Truncated: snapshot.Truncated,
	}
}

func reliabilityStatusToProto(status adminkernel.ReliabilityStatus) adminv1.AdminReliabilityStatus {
	switch status {
	case adminkernel.ReliabilityStatusOK:
		return adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_OK
	case adminkernel.ReliabilityStatusDegraded:
		return adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_DEGRADED
	default:
		return adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_UNSPECIFIED
	}
}

func reliabilitySignalCodeToProto(code adminkernel.ReliabilitySignalCode) adminv1.AdminReliabilitySignalCode {
	switch code {
	case adminkernel.ReliabilitySignalConsistencyIssues:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_CONSISTENCY_ISSUES
	case adminkernel.ReliabilitySignalAllocationLifecycleRetries:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_ALLOCATION_LIFECYCLE_RETRIES
	case adminkernel.ReliabilitySignalReconcileFailures:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_RECONCILE_FAILURES
	case adminkernel.ReliabilitySignalStorageBindings:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_STORAGE_BINDINGS
	case adminkernel.ReliabilitySignalNodeVolumeManagers:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_NODE_VOLUME_MANAGERS
	case adminkernel.ReliabilitySignalNodeFleet:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_NODE_FLEET
	default:
		return adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_UNSPECIFIED
	}
}

func consistencyStatusToProto(status consistencykernel.Status) adminv1.ConsistencyStatus {
	switch status {
	case consistencykernel.StatusOK:
		return adminv1.ConsistencyStatus_CONSISTENCY_STATUS_OK
	case consistencykernel.StatusInconsistent:
		return adminv1.ConsistencyStatus_CONSISTENCY_STATUS_INCONSISTENT
	default:
		return adminv1.ConsistencyStatus_CONSISTENCY_STATUS_UNSPECIFIED
	}
}

func consistencyIssueSeverityToProto(severity consistencykernel.Severity) adminv1.ConsistencyIssueSeverity {
	switch severity {
	case consistencykernel.SeverityWarning:
		return adminv1.ConsistencyIssueSeverity_CONSISTENCY_ISSUE_SEVERITY_WARNING
	case consistencykernel.SeverityError:
		return adminv1.ConsistencyIssueSeverity_CONSISTENCY_ISSUE_SEVERITY_ERROR
	default:
		return adminv1.ConsistencyIssueSeverity_CONSISTENCY_ISSUE_SEVERITY_UNSPECIFIED
	}
}

func consistencyIssueCodeToProto(code consistencykernel.IssueCode) adminv1.ConsistencyIssueCode {
	switch code {
	case consistencykernel.IssueActiveReservationMissingAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_RESERVATION_MISSING_ALLOCATION
	case consistencykernel.IssueActiveReservationOnEndedAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_RESERVATION_ON_ENDED_ALLOCATION
	case consistencykernel.IssueActiveReservationAllocationMismatch:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_RESERVATION_ALLOCATION_MISMATCH
	case consistencykernel.IssueActiveLeaseMissingAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_LEASE_MISSING_ALLOCATION
	case consistencykernel.IssueActiveLeaseOnEndedAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_LEASE_ON_ENDED_ALLOCATION
	case consistencykernel.IssueActiveLeaseAllocationNodeMismatch:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_LEASE_ALLOCATION_NODE_MISMATCH
	case consistencykernel.IssueActiveTunnelMissingAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_TUNNEL_MISSING_ALLOCATION
	case consistencykernel.IssueActiveTunnelOnEndedAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_TUNNEL_ON_ENDED_ALLOCATION
	case consistencykernel.IssueActiveTunnelAllocationNodeMismatch:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_TUNNEL_ALLOCATION_NODE_MISMATCH
	case consistencykernel.IssueServiceReferenceMissingAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_SERVICE_REFERENCE_MISSING_ALLOCATION
	case consistencykernel.IssueServiceReferenceEndedAllocation:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_SERVICE_REFERENCE_ENDED_ALLOCATION
	case consistencykernel.IssueServiceReferenceOwnerMismatch:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_SERVICE_REFERENCE_OWNER_MISMATCH
	default:
		return adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_UNSPECIFIED
	}
}

func consistencyRepairOwnerToProto(owner consistencykernel.RepairOwner) adminv1.ConsistencyRepairOwner {
	switch owner {
	case consistencykernel.RepairOwnerWorkloadController:
		return adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_WORKLOAD_CONTROLLER
	case consistencykernel.RepairOwnerNodeLifecycle:
		return adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_NODE_LIFECYCLE
	case consistencykernel.RepairOwnerTunnelController:
		return adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_TUNNEL_CONTROLLER
	case consistencykernel.RepairOwnerServiceController:
		return adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_SERVICE_CONTROLLER
	case consistencykernel.RepairOwnerAdminOperatorTriage:
		return adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_ADMIN_OPERATOR_TRIAGE
	default:
		return adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_UNSPECIFIED
	}
}

func consistencyRepairActionToProto(action consistencykernel.RepairAction) adminv1.ConsistencyRepairAction {
	switch action {
	case consistencykernel.RepairActionWorkloadCleanup:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_WORKLOAD_CLEANUP
	case consistencykernel.RepairActionWorkloadCleanupAndReadmit:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_WORKLOAD_CLEANUP_AND_READMIT
	case consistencykernel.RepairActionNodeLifecycleReconcile:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_NODE_LIFECYCLE_RECONCILE
	case consistencykernel.RepairActionTunnelLifecycleReconcile:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_TUNNEL_LIFECYCLE_RECONCILE
	case consistencykernel.RepairActionServiceReconcile:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_SERVICE_RECONCILE
	case consistencykernel.RepairActionAdminTriage:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_ADMIN_TRIAGE
	default:
		return adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_UNSPECIFIED
	}
}

func consistencyRepairTargetTypeToProto(targetType consistencykernel.RepairTargetType) adminv1.ConsistencyRepairTargetType {
	switch targetType {
	case consistencykernel.RepairTargetTypeAllocation:
		return adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_ALLOCATION
	case consistencykernel.RepairTargetTypeRun:
		return adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_RUN
	case consistencykernel.RepairTargetTypeService:
		return adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_SERVICE
	case consistencykernel.RepairTargetTypeTunnelSession:
		return adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_TUNNEL_SESSION
	default:
		return adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_UNSPECIFIED
	}
}
