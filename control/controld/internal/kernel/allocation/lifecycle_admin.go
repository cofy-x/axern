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
		DueOnly: in.DueOnly,
		Limit:   in.Limit,
	}
	if out.Limit <= 0 {
		out.Limit = DefaultLifecycleRetryListLimit
	}
	if out.Limit > MaxLifecycleRetryListLimit {
		out.Limit = MaxLifecycleRetryListLimit
	}
	return out
}

func NormalizeForceLifecycleRetryRequest(in ForceLifecycleRetryRequest) ForceLifecycleRetryRequest {
	return ForceLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(in.AllocationID),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
		RequestedRunAt: in.RequestedRunAt,
	}
}

func ValidateForceLifecycleRetryRequest(req ForceLifecycleRetryRequest) error {
	if req.AllocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	return nil
}

func NormalizeFailLifecycleRetryRequest(in FailLifecycleRetryRequest) FailLifecycleRetryRequest {
	return FailLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(in.AllocationID),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
	}
}

func ValidateFailLifecycleRetryRequest(req FailLifecycleRetryRequest) error {
	if req.AllocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	return nil
}

func NormalizeClearLifecycleRetryRequest(in ClearLifecycleRetryRequest) ClearLifecycleRetryRequest {
	return ClearLifecycleRetryRequest{
		AllocationID:   strings.TrimSpace(in.AllocationID),
		OperatorReason: strings.TrimSpace(in.OperatorReason),
	}
}

func ValidateClearLifecycleRetryRequest(req ClearLifecycleRetryRequest) error {
	if req.AllocationID == "" {
		return grpcstatus.Error(codes.InvalidArgument, "allocation_id is required")
	}
	if req.OperatorReason == "" {
		return grpcstatus.Error(codes.InvalidArgument, "operator_reason is required")
	}
	return nil
}
