package pgservice

import (
	"strings"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type serviceAllocationStatus struct {
	Status commonv1.AllocationStatus
	Ready  bool
}

type observedHealth struct {
	status            servicev1.ServiceStatus
	readyReplicas     int32
	unhealthyReplicas int32
}

func computeServiceStatus(service *servicev1.Service) servicev1.ServiceStatus {
	return deriveObservedHealth(service, nil).status
}

func deriveObservedHealth(service *servicev1.Service, allocations []*serviceAllocationStatus) observedHealth {
	if service == nil {
		return observedHealth{status: servicev1.ServiceStatus_SERVICE_STATUS_UNSPECIFIED}
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING || service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		return observedHealth{status: service.GetStatus()}
	}
	if service.GetReplicas() == 0 && len(service.GetAllocationIds()) == 0 {
		return observedHealth{
			status:            servicev1.ServiceStatus_SERVICE_STATUS_READY,
			readyReplicas:     0,
			unhealthyReplicas: 0,
		}
	}
	var health observedHealth
	for _, alloc := range allocations {
		if alloc == nil {
			continue
		}
		switch alloc.Status {
		case commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING:
			if alloc.Ready {
				health.readyReplicas++
			}
		case commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED:
			health.unhealthyReplicas++
		}
	}
	if health.readyReplicas == service.GetReplicas() && len(service.GetAllocationIds()) == int(service.GetReplicas()) {
		health.status = servicev1.ServiceStatus_SERVICE_STATUS_READY
		health.unhealthyReplicas = 0
		return health
	}
	if service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED && len(service.GetAllocationIds()) == 0 {
		health.status = servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED
		return health
	}
	if health.unhealthyReplicas == 0 {
		health.unhealthyReplicas = service.GetUnhealthyReplicas()
	}
	if health.unhealthyReplicas > 0 {
		health.status = servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED
		return health
	}
	health.status = servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING
	return health
}

func allocationStatusesFromRecords(records []*servicekernel.AllocationRecord) []*serviceAllocationStatus {
	statuses := make([]*serviceAllocationStatus, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		statuses = append(statuses, &serviceAllocationStatus{Status: record.Status, Ready: record.Ready})
	}
	return statuses
}

func applyObservedHealth(service *servicev1.Service, health observedHealth) {
	if service == nil {
		return
	}
	service.Status = health.status
	service.ReadyReplicas = health.readyReplicas
	service.UnhealthyReplicas = health.unhealthyReplicas
}

func applyRolloutReconciliation(service *servicev1.Service, rollout *servicev1.ServiceRolloutStatus) {
	if service == nil {
		return
	}
	service.RolloutStatus = rollout
	if rollout != nil && rollout.GetInProgress() && service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_READY {
		service.Status = servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING
	}
}

func removeAllocationID(allocationIDs []string, allocationID string) []string {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return append([]string(nil), allocationIDs...)
	}
	out := make([]string, 0, len(allocationIDs))
	for _, current := range allocationIDs {
		if strings.TrimSpace(current) == allocationID {
			continue
		}
		out = append(out, current)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
