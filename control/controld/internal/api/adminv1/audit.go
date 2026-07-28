package adminv1

import (
	"context"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Server) ListAdminAuditEvents(ctx context.Context, req *adminv1.ListAdminAuditEventsRequest) (*adminv1.ListAdminAuditEventsResponse, error) {
	if s.deps.AdminAuditEvents == nil {
		return nil, grpcstatus.Error(codes.Unavailable, "admin audit is unavailable")
	}
	events, err := s.deps.AdminAuditEvents.ListAdminAuditEvents(ctx, auditEventFilterFromProto(req))
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.AdminAuditEvent, 0, len(events))
	for _, event := range events {
		out = append(out, auditEventToProto(event))
	}
	return &adminv1.ListAdminAuditEventsResponse{Events: out}, nil
}

func auditEventFilterFromProto(req *adminv1.ListAdminAuditEventsRequest) adminkernel.AuditEventFilter {
	if req == nil {
		return adminkernel.AuditEventFilter{}
	}
	filter := req.GetFilter()
	return adminkernel.AuditEventFilter{
		Operation:  auditOperationFromProto(filter.GetOperation()),
		TargetType: auditTargetTypeFromProto(filter.GetTargetType()),
		TargetID:   filter.GetTargetID(),
		Limit:      int(req.GetLimit()),
	}
}

func auditEventToProto(event adminkernel.AuditEvent) *adminv1.AdminAuditEvent {
	return &adminv1.AdminAuditEvent{
		EventID:        event.EventID,
		Operation:      auditOperationToProto(event.Operation),
		TargetType:     auditTargetTypeToProto(event.TargetType),
		TargetID:       event.TargetID,
		OperatorReason: event.OperatorReason,
		CreatedAt:      timestamppb.New(event.CreatedAt),
	}
}

func auditOperationFromProto(operation adminv1.AdminAuditOperation) string {
	switch operation {
	case adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY:
		return adminkernel.AuditOperationForceAllocationLifecycleRetry
	case adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FAIL_ALLOCATION_LIFECYCLE_RETRY:
		return adminkernel.AuditOperationFailAllocationLifecycleRetry
	case adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_CLEAR_ALLOCATION_LIFECYCLE_RETRY:
		return adminkernel.AuditOperationClearAllocationLifecycleRetry
	case adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETRY_STORAGE_BINDING:
		return adminkernel.AuditOperationRetryStorageBinding
	case adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_PURGE_SERVICE:
		return adminkernel.AuditOperationPurgeService
	case adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETIRE_NODE:
		return adminkernel.AuditOperationRetireNode
	default:
		return ""
	}
}

func auditOperationToProto(operation string) adminv1.AdminAuditOperation {
	switch operation {
	case adminkernel.AuditOperationForceAllocationLifecycleRetry:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY
	case adminkernel.AuditOperationFailAllocationLifecycleRetry:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FAIL_ALLOCATION_LIFECYCLE_RETRY
	case adminkernel.AuditOperationClearAllocationLifecycleRetry:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_CLEAR_ALLOCATION_LIFECYCLE_RETRY
	case adminkernel.AuditOperationRetryStorageBinding:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETRY_STORAGE_BINDING
	case adminkernel.AuditOperationPurgeService:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_PURGE_SERVICE
	case adminkernel.AuditOperationRetireNode:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETIRE_NODE
	default:
		return adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_UNSPECIFIED
	}
}

func auditTargetTypeFromProto(targetType adminv1.AdminAuditTargetType) string {
	switch targetType {
	case adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION:
		return adminkernel.AuditTargetAllocation
	case adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_STORAGE_BINDING:
		return adminkernel.AuditTargetStorageBinding
	case adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_SERVICE:
		return adminkernel.AuditTargetService
	case adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_NODE:
		return adminkernel.AuditTargetNode
	default:
		return ""
	}
}

func auditTargetTypeToProto(targetType string) adminv1.AdminAuditTargetType {
	switch targetType {
	case adminkernel.AuditTargetAllocation:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION
	case adminkernel.AuditTargetStorageBinding:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_STORAGE_BINDING
	case adminkernel.AuditTargetService:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_SERVICE
	case adminkernel.AuditTargetNode:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_NODE
	default:
		return adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_UNSPECIFIED
	}
}
