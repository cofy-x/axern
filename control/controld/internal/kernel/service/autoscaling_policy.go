package servicekernel

import (
	"strings"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func normalizeAutoscalingPolicy(in *servicev1.ServiceAutoscalingPolicy) *servicev1.ServiceAutoscalingPolicy {
	if in == nil {
		return nil
	}
	out := proto.Clone(in).(*servicev1.ServiceAutoscalingPolicy)
	for _, schedule := range out.GetSchedules() {
		if schedule == nil {
			continue
		}
		schedule.Name = strings.TrimSpace(schedule.GetName())
		schedule.CronUtc = strings.TrimSpace(schedule.GetCronUtc())
	}
	return out
}

func NormalizeAutoscalingPolicy(in *servicev1.ServiceAutoscalingPolicy) *servicev1.ServiceAutoscalingPolicy {
	return normalizeAutoscalingPolicy(in)
}

func validateAndNormalizeAutoscalingPolicy(in *servicev1.ServiceAutoscalingPolicy) (*servicev1.ServiceAutoscalingPolicy, error) {
	if in == nil {
		return nil, nil
	}
	if in.GetMinReplicas() < 0 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service autoscaling_policy.min_replicas must be >= 0")
	}
	if in.GetMaxReplicas() < 1 {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service autoscaling_policy.max_replicas must be >= 1")
	}
	if in.GetMinReplicas() > in.GetMaxReplicas() {
		return nil, grpcstatus.Error(codes.InvalidArgument, "service autoscaling_policy.min_replicas must be <= max_replicas")
	}
	out := normalizeAutoscalingPolicy(in)
	parser := autoscalingCronParser()
	for idx, schedule := range out.GetSchedules() {
		field := "service autoscaling_policy.schedules"
		if schedule == nil {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "%s[%d] is required", field, idx)
		}
		if schedule.GetName() == "" {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "%s[%d].name is required", field, idx)
		}
		if schedule.GetCronUtc() == "" {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "%s[%d].cron_utc is required", field, idx)
		}
		if _, err := parser.Parse(schedule.GetCronUtc()); err != nil {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "%s[%d].cron_utc is invalid: %v", field, idx, err)
		}
		if schedule.GetReplicas() < out.GetMinReplicas() || schedule.GetReplicas() > out.GetMaxReplicas() {
			return nil, grpcstatus.Errorf(codes.InvalidArgument, "%s[%d].replicas must be within min_replicas and max_replicas", field, idx)
		}
	}
	return out, nil
}

func ValidateAndNormalizeAutoscalingPolicy(in *servicev1.ServiceAutoscalingPolicy) (*servicev1.ServiceAutoscalingPolicy, error) {
	return validateAndNormalizeAutoscalingPolicy(in)
}

func normalizeAutoscalingStatus(in *servicev1.ServiceAutoscalingStatus) *servicev1.ServiceAutoscalingStatus {
	if in == nil {
		return nil
	}
	out := cloneAutoscalingStatus(in)
	out.ActiveScheduleName = strings.TrimSpace(out.GetActiveScheduleName())
	out.Message = strings.TrimSpace(out.GetMessage())
	return out
}

func NormalizeAutoscalingStatus(in *servicev1.ServiceAutoscalingStatus) *servicev1.ServiceAutoscalingStatus {
	return normalizeAutoscalingStatus(in)
}
