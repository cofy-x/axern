package adminkernel

import (
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	AuditOperationForceAllocationLifecycleRetry = "force_allocation_lifecycle_retry"
	AuditOperationFailAllocationLifecycleRetry  = "fail_allocation_lifecycle_retry"
	AuditOperationClearAllocationLifecycleRetry = "clear_allocation_lifecycle_retry"
	AuditOperationRetryStorageBinding           = "retry_storage_binding"
	AuditOperationPurgeService                  = "purge_service"
	AuditOperationRetireNode                    = "retire_node"

	AuditTargetAllocation     = "allocation"
	AuditTargetStorageBinding = "storage_binding"
	AuditTargetService        = "service"
	AuditTargetNode           = "node"

	MaxAuditEventListLimit     = 100
	DefaultAuditEventListLimit = 50
)

type AuditEventFilter struct {
	Operation  string
	TargetType string
	TargetID   string
	Limit      int
}

type AuditEvent struct {
	EventID        string
	Operation      string
	TargetType     string
	TargetID       string
	OperatorReason string
	CreatedAt      time.Time
}

func NormalizeAuditEventFilter(in AuditEventFilter) AuditEventFilter {
	out := AuditEventFilter{
		Operation:  strings.TrimSpace(in.Operation),
		TargetType: strings.TrimSpace(in.TargetType),
		TargetID:   strings.TrimSpace(in.TargetID),
		Limit:      in.Limit,
	}
	if out.Limit <= 0 {
		out.Limit = DefaultAuditEventListLimit
	}
	if out.Limit > MaxAuditEventListLimit {
		out.Limit = MaxAuditEventListLimit
	}
	return out
}

func ValidateAuditEventFilter(filter AuditEventFilter) error {
	switch filter.Operation {
	case "", AuditOperationForceAllocationLifecycleRetry, AuditOperationFailAllocationLifecycleRetry, AuditOperationClearAllocationLifecycleRetry, AuditOperationRetryStorageBinding, AuditOperationPurgeService, AuditOperationRetireNode:
	default:
		return grpcstatus.Errorf(codes.InvalidArgument, "unsupported admin audit operation %q", filter.Operation)
	}
	switch filter.TargetType {
	case "", AuditTargetAllocation, AuditTargetStorageBinding, AuditTargetService, AuditTargetNode:
	default:
		return grpcstatus.Errorf(codes.InvalidArgument, "unsupported admin audit target_type %q", filter.TargetType)
	}
	if filter.TargetID != "" && filter.TargetType == "" {
		return grpcstatus.Error(codes.InvalidArgument, "admin audit target_type is required when target_id is set")
	}
	return nil
}
