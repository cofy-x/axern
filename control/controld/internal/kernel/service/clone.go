package servicekernel

import (
	"maps"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/proto"
)

func cloneService(in *servicev1.Service) *servicev1.Service {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*servicev1.Service)
}

func CloneService(in *servicev1.Service) *servicev1.Service {
	return cloneService(in)
}

func cloneConfig(in *commonv1.ExecutionConfig) *commonv1.ExecutionConfig {
	if in == nil {
		return &commonv1.ExecutionConfig{}
	}
	return proto.Clone(in).(*commonv1.ExecutionConfig)
}

func cloneRolloutPolicy(in *servicev1.ServiceRolloutPolicy) *servicev1.ServiceRolloutPolicy {
	if in == nil {
		return defaultRolloutPolicy()
	}
	return normalizeRolloutPolicy(proto.Clone(in).(*servicev1.ServiceRolloutPolicy))
}

func CloneRolloutPolicy(in *servicev1.ServiceRolloutPolicy) *servicev1.ServiceRolloutPolicy {
	return cloneRolloutPolicy(in)
}

func cloneProbe(in *servicev1.ServiceProbe) *servicev1.ServiceProbe {
	if in == nil {
		return nil
	}
	return normalizeProbe(proto.Clone(in).(*servicev1.ServiceProbe))
}

func cloneAutoscalingPolicy(in *servicev1.ServiceAutoscalingPolicy) *servicev1.ServiceAutoscalingPolicy {
	if in == nil {
		return nil
	}
	return normalizeAutoscalingPolicy(proto.Clone(in).(*servicev1.ServiceAutoscalingPolicy))
}

func CloneAutoscalingPolicy(in *servicev1.ServiceAutoscalingPolicy) *servicev1.ServiceAutoscalingPolicy {
	return cloneAutoscalingPolicy(in)
}

func cloneAutoscalingStatus(in *servicev1.ServiceAutoscalingStatus) *servicev1.ServiceAutoscalingStatus {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*servicev1.ServiceAutoscalingStatus)
}

func CloneAutoscalingStatus(in *servicev1.ServiceAutoscalingStatus) *servicev1.ServiceAutoscalingStatus {
	return cloneAutoscalingStatus(in)
}

func cloneDeletionStatus(in *servicev1.ServiceDeletionStatus) *servicev1.ServiceDeletionStatus {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*servicev1.ServiceDeletionStatus)
}

func CloneDeletionStatus(in *servicev1.ServiceDeletionStatus) *servicev1.ServiceDeletionStatus {
	return cloneDeletionStatus(in)
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
