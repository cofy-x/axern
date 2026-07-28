package pgservice

import (
	"fmt"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func serviceSelectSQL() string {
	return `SELECT service_id, namespace, environment_id, replicas, ready_replicas, unhealthy_replicas, rollout_policy, readiness_probe, liveness_probe, autoscaling_policy, autoscaling_status, status, config,
		allocation_ids, labels, version, created_at, updated_at, message, deletion_status FROM services`
}

type serviceScanner interface {
	Scan(dest ...any) error
}

func scanService(row serviceScanner) (*servicev1.Service, error) {
	var (
		service                                                  servicev1.Service
		statusText                                               string
		configJSON, allocationIDsJSON, labelsJSON                []byte
		rolloutPolicyJSON, readinessProbeJSON, livenessProbeJSON []byte
		autoscalingPolicyJSON, autoscalingStatusJSON             []byte
		deletionStatusJSON                                       []byte
		createdAt, updatedAt                                     time.Time
	)
	if err := row.Scan(&service.ID, &service.Namespace, &service.EnvironmentID, &service.Replicas, &service.ReadyReplicas, &service.UnhealthyReplicas, &rolloutPolicyJSON, &readinessProbeJSON, &livenessProbeJSON, &autoscalingPolicyJSON, &autoscalingStatusJSON, &statusText, &configJSON, &allocationIDsJSON, &labelsJSON, &service.Version, &createdAt, &updatedAt, &service.Message, &deletionStatusJSON); err != nil {
		return nil, err
	}
	service.Status = parseServiceStatus(statusText)
	service.Config = &commonv1.ExecutionConfig{}
	if err := protojson.Unmarshal(configJSON, service.Config); err != nil {
		return nil, fmt.Errorf("unmarshal service config: %w", err)
	}
	service.RolloutPolicy = servicekernel.NormalizeRolloutPolicy(&servicev1.ServiceRolloutPolicy{})
	if len(rolloutPolicyJSON) > 0 {
		service.RolloutPolicy = &servicev1.ServiceRolloutPolicy{}
		if err := protojson.Unmarshal(rolloutPolicyJSON, service.RolloutPolicy); err != nil {
			return nil, fmt.Errorf("unmarshal service rollout policy: %w", err)
		}
		service.RolloutPolicy = servicekernel.NormalizeRolloutPolicy(service.RolloutPolicy)
	}
	if len(readinessProbeJSON) > 0 && string(readinessProbeJSON) != "null" {
		service.ReadinessProbe = &servicev1.ServiceProbe{}
		if err := protojson.Unmarshal(readinessProbeJSON, service.ReadinessProbe); err != nil {
			return nil, fmt.Errorf("unmarshal service readiness probe: %w", err)
		}
		service.ReadinessProbe = servicekernel.NormalizeReadinessProbe(service.ReadinessProbe)
	}
	if len(livenessProbeJSON) > 0 && string(livenessProbeJSON) != "null" {
		service.LivenessProbe = &servicev1.ServiceProbe{}
		if err := protojson.Unmarshal(livenessProbeJSON, service.LivenessProbe); err != nil {
			return nil, fmt.Errorf("unmarshal service liveness probe: %w", err)
		}
		service.LivenessProbe = servicekernel.NormalizeLivenessProbe(service.LivenessProbe)
	}
	if len(autoscalingPolicyJSON) > 0 && string(autoscalingPolicyJSON) != "null" {
		service.AutoscalingPolicy = &servicev1.ServiceAutoscalingPolicy{}
		if err := protojson.Unmarshal(autoscalingPolicyJSON, service.AutoscalingPolicy); err != nil {
			return nil, fmt.Errorf("unmarshal service autoscaling policy: %w", err)
		}
		service.AutoscalingPolicy = servicekernel.NormalizeAutoscalingPolicy(service.AutoscalingPolicy)
	}
	if len(autoscalingStatusJSON) > 0 && string(autoscalingStatusJSON) != "null" {
		service.AutoscalingStatus = &servicev1.ServiceAutoscalingStatus{}
		if err := protojson.Unmarshal(autoscalingStatusJSON, service.AutoscalingStatus); err != nil {
			return nil, fmt.Errorf("unmarshal service autoscaling status: %w", err)
		}
		service.AutoscalingStatus = servicekernel.NormalizeAutoscalingStatus(service.AutoscalingStatus)
	}
	if len(deletionStatusJSON) > 0 && string(deletionStatusJSON) != "null" {
		service.DeletionStatus = &servicev1.ServiceDeletionStatus{}
		if err := protojson.Unmarshal(deletionStatusJSON, service.DeletionStatus); err != nil {
			return nil, fmt.Errorf("unmarshal service deletion status: %w", err)
		}
	}
	service.AllocationIds = unmarshalStringSlice(allocationIDsJSON)
	service.Labels = unmarshalJSONMap(labelsJSON)
	service.CreatedAt = timestamppb.New(createdAt)
	service.UpdatedAt = timestamppb.New(updatedAt)
	return &service, nil
}

func parseServiceStatus(value string) servicev1.ServiceStatus {
	if n, ok := servicev1.ServiceStatus_value[value]; ok {
		return servicev1.ServiceStatus(n)
	}
	return servicev1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED
}
