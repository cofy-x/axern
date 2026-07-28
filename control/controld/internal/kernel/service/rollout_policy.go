package servicekernel

import (
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func defaultRolloutPolicy() *servicev1.ServiceRolloutPolicy {
	return &servicev1.ServiceRolloutPolicy{
		MaxSurge:       1,
		MaxUnavailable: 0,
	}
}

func normalizeRolloutPolicy(in *servicev1.ServiceRolloutPolicy) *servicev1.ServiceRolloutPolicy {
	if in == nil {
		return defaultRolloutPolicy()
	}
	out := proto.Clone(in).(*servicev1.ServiceRolloutPolicy)
	if out.GetMaxSurge() < 0 {
		out.MaxSurge = 0
	}
	if out.GetMaxUnavailable() < 0 {
		out.MaxUnavailable = 0
	}
	return out
}

func NormalizeRolloutPolicy(in *servicev1.ServiceRolloutPolicy) *servicev1.ServiceRolloutPolicy {
	return normalizeRolloutPolicy(in)
}

func validateAndNormalizeRolloutPolicy(in *servicev1.ServiceRolloutPolicy) (*servicev1.ServiceRolloutPolicy, error) {
	if in == nil {
		return defaultRolloutPolicy(), nil
	}
	if in.GetMaxSurge() < 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service rollout_policy.max_surge must be >= 0")
	}
	if in.GetMaxUnavailable() < 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service rollout_policy.max_unavailable must be >= 0")
	}
	return normalizeRolloutPolicy(in), nil
}

func ValidateAndNormalizeRolloutPolicy(in *servicev1.ServiceRolloutPolicy) (*servicev1.ServiceRolloutPolicy, error) {
	return validateAndNormalizeRolloutPolicy(in)
}
