package servicekernel

import (
	"strings"
	"time"

	environmentkernel "github.com/cofy-x/axern/control/controld/internal/kernel/environment"
	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func NewService(namespace, environmentID string, replicas int32, config *commonv1.ExecutionConfig, labels map[string]string, rolloutPolicy *servicev1.ServiceRolloutPolicy, readinessProbe, livenessProbe *servicev1.ServiceProbe, autoscaling *servicev1.ServiceAutoscalingPolicy, now time.Time) *servicev1.Service {
	service := &servicev1.Service{
		ID:                "svc-" + uuid.NewString(),
		Namespace:         environmentkernel.NormalizeNamespace(namespace),
		EnvironmentID:     strings.TrimSpace(environmentID),
		Replicas:          replicas,
		ReadyReplicas:     0,
		UnhealthyReplicas: 0,
		RolloutPolicy:     cloneRolloutPolicy(rolloutPolicy),
		Status:            servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING,
		Config:            executionkernel.NormalizeConfig(config),
		ReadinessProbe:    cloneProbe(readinessProbe),
		LivenessProbe:     cloneProbe(livenessProbe),
		AutoscalingPolicy: cloneAutoscalingPolicy(autoscaling),
		Labels:            cloneLabels(labels),
		Version:           1,
		CreatedAt:         timestamppb.New(now),
		UpdatedAt:         timestamppb.New(now),
	}
	service.Status = computeServiceStatus(service)
	return service
}

func ApplyUpdate(current *servicev1.Service, req *servicev1.UpdateServiceRequest, now time.Time) (*servicev1.Service, error) {
	if current == nil {
		return nil, nil
	}
	next := cloneService(current)
	if expected := req.GetExpectedVersion(); expected > 0 && next.GetVersion() != expected {
		return nil, grpcstatus.Errorf(codes.Aborted, "service version mismatch: got %d want %d", next.GetVersion(), expected)
	}
	if next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "service %q is deleting or deleted", next.GetID())
	}
	updatePaths := map[string]bool{}
	for _, path := range req.GetUpdateMask().GetPaths() {
		updatePaths[strings.TrimSpace(path)] = true
	}
	updateAll := len(updatePaths) == 0
	if updateAll || updatePaths["replicas"] {
		if req.Replicas != nil {
			next.Replicas = req.GetReplicas()
		}
	}
	if updateAll || updatePaths["config"] {
		if req.GetConfig() != nil {
			next.Config = executionkernel.NormalizeConfig(req.GetConfig())
		}
	}
	if updateAll || updatePaths["environment_id"] {
		if req.EnvironmentID != nil {
			next.EnvironmentID = strings.TrimSpace(req.GetEnvironmentID())
		}
	}
	if updateAll || updatePaths["labels"] {
		next.Labels = cloneLabels(req.GetLabels())
	}
	if (updateAll && req.GetRolloutPolicy() != nil) || updatePaths["rollout_policy"] {
		policy, err := validateAndNormalizeRolloutPolicy(req.GetRolloutPolicy())
		if err != nil {
			return nil, err
		}
		next.RolloutPolicy = policy
	}
	if updatePaths["readiness_probe"] {
		probe, err := validateAndNormalizeProbe("readiness_probe", req.GetReadinessProbe())
		if err != nil {
			return nil, err
		}
		next.ReadinessProbe = probe
	} else if updateAll && req.GetReadinessProbe() != nil {
		probe, err := validateAndNormalizeProbe("readiness_probe", req.GetReadinessProbe())
		if err != nil {
			return nil, err
		}
		next.ReadinessProbe = probe
	}
	if updatePaths["liveness_probe"] {
		probe, err := validateAndNormalizeProbe("liveness_probe", req.GetLivenessProbe())
		if err != nil {
			return nil, err
		}
		next.LivenessProbe = probe
	} else if updateAll && req.GetLivenessProbe() != nil {
		probe, err := validateAndNormalizeProbe("liveness_probe", req.GetLivenessProbe())
		if err != nil {
			return nil, err
		}
		next.LivenessProbe = probe
	}
	if updatePaths["autoscaling_policy"] {
		policy, err := validateAndNormalizeAutoscalingPolicy(req.GetAutoscalingPolicy())
		if err != nil {
			return nil, err
		}
		next.AutoscalingPolicy = policy
		if policy == nil {
			next.AutoscalingStatus = nil
		}
	} else if updateAll && req.GetAutoscalingPolicy() != nil {
		policy, err := validateAndNormalizeAutoscalingPolicy(req.GetAutoscalingPolicy())
		if err != nil {
			return nil, err
		}
		next.AutoscalingPolicy = policy
	}
	next.Status = computeServiceStatus(next)
	next.Version++
	next.UpdatedAt = timestamppb.New(now)
	return next, nil
}

func computeServiceStatus(service *servicev1.Service) servicev1.ServiceStatus {
	if service == nil {
		return servicev1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		return service.GetStatus()
	}
	if service.GetReplicas() == 0 && len(service.GetAllocationIds()) == 0 {
		return servicev1.ServiceStatus_SERVICE_STATUS_READY
	}
	if service.GetReadyReplicas() == service.GetReplicas() && len(service.GetAllocationIds()) == int(service.GetReplicas()) {
		return servicev1.ServiceStatus_SERVICE_STATUS_READY
	}
	if service.GetUnhealthyReplicas() > 0 {
		return servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED
	}
	return servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING
}

func MarkDeleted(current *servicev1.Service, disposition servicev1.ServiceVolumeDisposition, now time.Time) *servicev1.Service {
	if current == nil {
		return nil
	}
	next := cloneService(current)
	next.Status = servicev1.ServiceStatus_SERVICE_STATUS_DELETING
	if disposition == servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_UNSPECIFIED {
		disposition = servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN
	}
	next.DeletionStatus = &servicev1.ServiceDeletionStatus{
		Phase:             servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RELEASING_ALLOCATIONS,
		VolumeDisposition: disposition,
		Message:           "releasing service allocations",
	}
	next.Version++
	next.UpdatedAt = timestamppb.New(now)
	return next
}

func ApplyDeletionProgress(current *servicev1.Service, deletion *servicev1.ServiceDeletionStatus, now time.Time) *servicev1.Service {
	if current == nil {
		return nil
	}
	next := cloneService(current)
	next.DeletionStatus = cloneDeletionStatus(deletion)
	if deletion.GetPhase() == servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE {
		next.Status = servicev1.ServiceStatus_SERVICE_STATUS_DELETED
	} else {
		next.Status = servicev1.ServiceStatus_SERVICE_STATUS_DELETING
	}
	next.Version++
	next.UpdatedAt = timestamppb.New(now)
	return next
}

// ApplyStatusUpdate applies an internal status projection while preserving the
// service lifecycle's terminal state. Reconcile work that started before a
// delete may finish afterward, but it must never resurrect the service.
func ApplyStatusUpdate(current *servicev1.Service, status servicev1.ServiceStatus, message string, now time.Time) (*servicev1.Service, bool) {
	if current == nil {
		return nil, false
	}
	next := cloneService(current)
	if current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		return next, false
	}
	next.Status = status
	next.Message = strings.TrimSpace(message)
	next.ReadyReplicas = current.GetReadyReplicas()
	next.UnhealthyReplicas = current.GetUnhealthyReplicas()
	next.AutoscalingPolicy = CloneAutoscalingPolicy(current.GetAutoscalingPolicy())
	next.AutoscalingStatus = CloneAutoscalingStatus(current.GetAutoscalingStatus())
	next.Version++
	next.UpdatedAt = timestamppb.New(now)
	return next, true
}

func MatchFilter(svc *servicev1.Service, filter *servicev1.ServiceListFilter) bool {
	if svc == nil {
		return false
	}
	if svc.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		if filter == nil || !containsServiceStatus(filter.GetStatuses(), servicev1.ServiceStatus_SERVICE_STATUS_DELETED) {
			return false
		}
	}
	if filter == nil {
		return true
	}
	if namespace := strings.TrimSpace(filter.GetNamespace()); namespace != "" && svc.GetNamespace() != environmentkernel.NormalizeNamespace(namespace) {
		return false
	}
	if len(filter.GetStatuses()) > 0 {
		matched := false
		for _, status := range filter.GetStatuses() {
			if svc.GetStatus() == status {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	} else if svc.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		return false
	}
	for key, value := range filter.GetLabels() {
		if svc.GetLabels()[strings.TrimSpace(key)] != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func containsServiceStatus(statuses []servicev1.ServiceStatus, wanted servicev1.ServiceStatus) bool {
	for _, status := range statuses {
		if status == wanted {
			return true
		}
	}
	return false
}
