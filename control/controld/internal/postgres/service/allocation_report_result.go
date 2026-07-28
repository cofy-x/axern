package pgservice

import (
	"time"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func buildAllocationStatusReports(current, next *servicev1.Service, transitions []*serviceStatusTransition, now time.Time) []*servicekernel.AllocationStatusReport {
	if current == nil || next == nil {
		return nil
	}
	reports := make([]*servicekernel.AllocationStatusReport, 0, len(transitions))
	for _, transition := range transitions {
		if transition == nil || transition.allocation == nil || transition.currentReady || !transition.nextReady {
			continue
		}
		report := &servicekernel.AllocationStatusReport{ReplicaBecameReady: true}
		if !transition.allocation.CreatedAt.IsZero() {
			report.ReplicaReadyDuration = now.Sub(transition.allocation.CreatedAt)
			report.ReplicaReadyDurationKnown = true
		}
		reports = append(reports, report)
	}
	if len(reports) == 0 || current.GetReplicas() <= 0 ||
		current.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_RECONCILING ||
		current.GetReadyReplicas() >= current.GetReplicas() ||
		next.GetReadyReplicas() < next.GetReplicas() {
		return reports
	}
	createdAt := current.GetCreatedAt().AsTime()
	if createdAt.IsZero() {
		return reports
	}
	report := reports[len(reports)-1]
	report.ServiceBecameReady = true
	report.ServiceReadyDuration = now.Sub(createdAt)
	report.ServiceReadyDurationKnown = true
	return reports
}
