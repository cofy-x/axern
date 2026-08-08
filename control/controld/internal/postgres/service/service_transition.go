package pgservice

import (
	"context"
	"strings"
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"github.com/jackc/pgx/v5"
)

type serviceObservationResult struct {
	service *servicev1.Service
	rollout *servicev1.ServiceRolloutStatus
}

func (s *PGStore) recordServiceObservationBatchEvents(ctx context.Context, tx pgx.Tx, current *servicev1.Service, result *serviceObservationResult, transitions []*serviceStatusTransition, now time.Time) error {
	if current == nil || result == nil || result.service == nil {
		return nil
	}
	next := result.service
	rollout := result.rollout
	for _, transition := range transitions {
		alloc := transition.allocation
		if transition.currentStatus != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING &&
			transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING &&
			servicekernel.AllocationMatchesDesired(alloc.DesiredSpecDigest, next) &&
			(len(current.GetAllocationIds()) > int(current.GetReplicas()) || current.GetUnhealthyReplicas() > 0 || current.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED) {
			if err := recordServiceEvent(ctx, tx, servicekernel.NewServiceEvent(
				next.GetID(),
				alloc.AllocationID,
				servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_RUNNING,
				phaseFromRollout(rollout),
				commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
				servicekernel.FirstNonEmpty(transition.message, "replacement replica is running"),
				now,
			)); err != nil {
				return err
			}
		}
		if transition.currentStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING &&
			transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING &&
			!transition.currentReady &&
			transition.nextReady &&
			servicekernel.AllocationMatchesDesired(alloc.DesiredSpecDigest, next) &&
			(len(current.GetAllocationIds()) > int(current.GetReplicas()) || current.GetUnhealthyReplicas() > 0 || current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY) {
			if err := recordServiceEvent(ctx, tx, servicekernel.NewServiceEvent(
				next.GetID(),
				alloc.AllocationID,
				servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_READY,
				phaseFromRollout(rollout),
				commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
				servicekernel.FirstNonEmpty(transition.message, "replacement replica is ready"),
				now,
			)); err != nil {
				return err
			}
		}
	}
	if len(transitions) == 0 {
		return nil
	}
	if current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED && next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		cause := selectServiceStatusTransition(transitions, func(transition *serviceStatusTransition) bool {
			return transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED ||
				transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED
		})
		diagnosticCode := workloadkernel.ClassifyDiagnostic(cause.nextStatus, next.GetMessage())
		if diagnosticCode == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_LIVENESS_PROBE_FAILED {
			if err := recordServiceEvent(ctx, tx, servicekernel.NewServiceEvent(
				next.GetID(),
				cause.allocation.AllocationID,
				servicev1.ServiceEventType_SERVICE_EVENT_TYPE_LIVENESS_FAILED,
				phaseFromRollout(rollout),
				diagnosticCode,
				next.GetMessage(),
				now,
			)); err != nil {
				return err
			}
		}
		if err := recordServiceEvent(ctx, tx, servicekernel.NewServiceEvent(
			next.GetID(),
			cause.allocation.AllocationID,
			servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_DEGRADED,
			phaseFromRollout(rollout),
			diagnosticCode,
			next.GetMessage(),
			now,
		)); err != nil {
			return err
		}
	}
	if current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY && next.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_READY {
		cause := selectServiceStatusTransition(transitions, func(transition *serviceStatusTransition) bool {
			return transition.nextReady
		})
		if err := recordServiceEvent(ctx, tx, servicekernel.NewServiceEvent(
			next.GetID(),
			cause.allocation.AllocationID,
			servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_RECOVERED,
			phaseFromRollout(rollout),
			commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
			"service recovered",
			now,
		)); err != nil {
			return err
		}
	}
	return nil
}

func applyObservedStatusBatchMessage(service *servicev1.Service, transitions []*serviceStatusTransition) {
	if service == nil || len(transitions) == 0 {
		return
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_READY {
		service.Message = ""
		return
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED {
		if transition := findServiceStatusTransition(transitions, func(transition *serviceStatusTransition) bool {
			return (transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED ||
				transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED) &&
				strings.TrimSpace(transition.message) != ""
		}); transition != nil && strings.TrimSpace(transition.message) != "" {
			service.Message = strings.TrimSpace(transition.message)
			return
		}
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING {
		if transition := findServiceStatusTransition(transitions, func(transition *serviceStatusTransition) bool {
			return transition.nextStatus == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING &&
				!transition.nextReady && strings.TrimSpace(transition.readinessMessage) != ""
		}); transition != nil && strings.TrimSpace(transition.readinessMessage) != "" {
			service.Message = strings.TrimSpace(transition.readinessMessage)
			return
		}
	}
	if transition := findServiceStatusTransition(transitions, func(transition *serviceStatusTransition) bool {
		return strings.TrimSpace(transition.message) != ""
	}); transition != nil {
		service.Message = strings.TrimSpace(transition.message)
	}
}

func selectServiceStatusTransition(transitions []*serviceStatusTransition, matches func(*serviceStatusTransition) bool) *serviceStatusTransition {
	if transition := findServiceStatusTransition(transitions, matches); transition != nil {
		return transition
	}
	return findServiceStatusTransition(transitions, nil)
}

func findServiceStatusTransition(transitions []*serviceStatusTransition, matches func(*serviceStatusTransition) bool) *serviceStatusTransition {
	for index := len(transitions) - 1; index >= 0; index-- {
		transition := transitions[index]
		if transition != nil && transition.allocation != nil && (matches == nil || matches(transition)) {
			return transition
		}
	}
	return nil
}

func phaseFromRollout(rollout *servicev1.ServiceRolloutStatus) servicev1.ServiceRolloutPhase {
	if rollout == nil {
		return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED
	}
	return rollout.GetPhase()
}
