package pgservice

import (
	"strings"

	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func buildReplicaView(record *serviceReplicaRecord, service *servicev1.Service) *servicev1.ServiceReplica {
	if record == nil || record.replica == nil {
		return nil
	}
	deriveReplicaState(record, service)
	return record.replica
}

func deriveReplicaState(record *serviceReplicaRecord, service *servicev1.Service) {
	if record == nil || record.replica == nil {
		return
	}
	record.replica.Outdated = servicekernel.AllocationOutdated(record.desiredSpecDigest, service)
	if record.replica.GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
		record.replica.Ready = false
	}
	diagnosticMessage := strings.TrimSpace(record.replica.GetMessage())
	if diagnosticMessage == "" {
		diagnosticMessage = strings.TrimSpace(record.replica.GetLifecycleRetry().GetLastError())
	}
	record.replica.DiagnosticCode = workloadkernel.ResolveDiagnostic(record.replica.GetDiagnosticCode(), record.replica.GetStatus(), diagnosticMessage)
}

func matchReplicaFilter(replica *servicev1.ServiceReplica, filter *servicev1.ServiceReplicaListFilter) bool {
	if replica == nil {
		return false
	}
	if !matchReplicaView(replica, filter) {
		return false
	}
	if filter == nil || len(filter.GetStatuses()) == 0 {
		return true
	}
	for _, status := range filter.GetStatuses() {
		if replica.GetStatus() == status {
			return true
		}
	}
	return false
}

func matchReplicaView(replica *servicev1.ServiceReplica, filter *servicev1.ServiceReplicaListFilter) bool {
	if replica == nil || filter == nil {
		return true
	}
	switch filter.GetView() {
	case servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNSPECIFIED,
		servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ALL:
		return true
	case servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_CURRENT:
		return !replica.GetEnded()
	case servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_ENDED:
		return replica.GetEnded()
	case servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UNHEALTHY:
		return replica.GetStatus() == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED ||
			replica.GetStatus() == commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED
	case servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_OUTDATED:
		return replica.GetOutdated()
	case servicev1.ServiceReplicaView_SERVICE_REPLICA_VIEW_UPDATED:
		return !replica.GetOutdated()
	default:
		return true
	}
}
