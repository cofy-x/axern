package allocationkernel

import (
	"strings"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	MaxLifecycleRetryListLimit     = 100
	DefaultLifecycleRetryListLimit = 50
)

func NormalizeLifecycleRetryFilter(in LifecycleRetryFilter) LifecycleRetryFilter {
	out := LifecycleRetryFilter{
		OwnerType: strings.TrimSpace(in.OwnerType),
		Reason:    strings.TrimSpace(in.Reason),
		DueOnly:   in.DueOnly,
		Limit:     in.Limit,
	}
	if out.Limit <= 0 {
		out.Limit = DefaultLifecycleRetryListLimit
	}
	if out.Limit > MaxLifecycleRetryListLimit {
		out.Limit = MaxLifecycleRetryListLimit
	}
	return out
}

func ValidateLifecycleRetryFilter(filter LifecycleRetryFilter) error {
	switch filter.OwnerType {
	case "", OwnerRun, OwnerService:
	default:
		return grpcstatus.Errorf(codes.InvalidArgument, "unsupported lifecycle retry owner_type %q", filter.OwnerType)
	}
	switch filter.Reason {
	case "", ReconcileReasonCreate, ReconcileReasonDelete:
	default:
		return grpcstatus.Errorf(codes.InvalidArgument, "unsupported lifecycle retry reason %q", filter.Reason)
	}
	return nil
}

func NormalizeForceLifecycleRetryRequest(in ForceLifecycleRetryRequest) ForceLifecycleRetryRequest {
	return ForceLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(in.AllocationID),
		Reason:         strings.TrimSpace(in.Reason),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
		RequestedRunAt: in.RequestedRunAt,
	}
}

func ValidateForceLifecycleRetryRequest(req ForceLifecycleRetryRequest) error {
	if req.AllocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	switch req.Reason {
	case ReconcileReasonCreate, ReconcileReasonDelete:
	default:
		return grpcstatus.Error(codes.InvalidArgument, "reason must be create or delete")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	return nil
}

func NormalizeFailLifecycleRetryRequest(in FailLifecycleRetryRequest) FailLifecycleRetryRequest {
	return FailLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(in.AllocationID),
		Reason:         strings.TrimSpace(in.Reason),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
	}
}

func ValidateFailLifecycleRetryRequest(req FailLifecycleRetryRequest) error {
	if req.AllocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if req.Reason != ReconcileReasonCreate {
		return grpcstatus.Error(codes.InvalidArgument, "reason must be create")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	return nil
}

func NormalizeClearLifecycleRetryRequest(in ClearLifecycleRetryRequest) ClearLifecycleRetryRequest {
	return ClearLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(in.AllocationID),
		Reason:         strings.TrimSpace(in.Reason),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
	}
}

func ValidateClearLifecycleRetryRequest(req ClearLifecycleRetryRequest) error {
	if req.AllocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	switch req.Reason {
	case ReconcileReasonCreate, ReconcileReasonDelete:
	default:
		return grpcstatus.Error(codes.InvalidArgument, "reason must be create or delete")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	return nil
}
