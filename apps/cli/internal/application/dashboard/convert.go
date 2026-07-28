package dashboard

import (
	"maps"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/cli/internal/workloaddiagnostic"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func NewServiceDTO(svc *servicev1.Service) ServiceDTO {
	if svc == nil {
		return ServiceDTO{}
	}
	rollout := svc.GetRolloutStatus()
	return ServiceDTO{
		ID:                svc.GetID(),
		Namespace:         svc.GetNamespace(),
		EnvironmentID:     svc.GetEnvironmentID(),
		Status:            enumLabel(svc.GetStatus().String(), "SERVICE_STATUS_"),
		Replicas:          svc.GetReplicas(),
		ReadyReplicas:     svc.GetReadyReplicas(),
		UnhealthyReplicas: svc.GetUnhealthyReplicas(),
		RuntimeClass:      svc.GetConfig().GetRuntimeClass(),
		Resources:         newResourceSpecDTO(svc.GetConfig().GetResources()),
		Message:           svc.GetMessage(),
		Labels:            cloneMap(svc.GetLabels()),
		RolloutPhase:      enumLabel(rollout.GetPhase().String(), "SERVICE_ROLLOUT_PHASE_"),
		DiagnosticCode:    serviceWorkloadDiagnosticLabel(firstWorkloadDiagnosticCode(svc.GetDiagnosticCode(), rollout.GetDiagnosticCode()), svc.GetMessage()),
		DiagnosticMessage: firstNonEmpty(rollout.GetDiagnosticMessage(), svc.GetMessage()),
		AdmissionSummary:  workloaddiagnostic.AdmissionBlockedSummary(svc.GetMessage()),
		CreatedAt:         formatTime(svc.GetCreatedAt()),
		UpdatedAt:         formatTime(svc.GetUpdatedAt()),
	}
}

func newResourceSpecDTO(resources *commonv1.ResourceSpec) *ResourceSpecDTO {
	if resources == nil {
		return nil
	}
	out := &ResourceSpecDTO{
		Requests: newResourceQuantityDTO(resources.GetRequests()),
		Limits:   newResourceQuantityDTO(resources.GetLimits()),
	}
	if out.Requests == nil && out.Limits == nil {
		return nil
	}
	return out
}

func newResourceQuantityDTO(quantity *commonv1.ResourceQuantity) *ResourceQuantityDTO {
	if quantity == nil {
		return nil
	}
	out := &ResourceQuantityDTO{
		CPUMilli:    quantity.GetCpuMilli(),
		MemoryBytes: quantity.GetMemoryBytes(),
	}
	if out.CPUMilli == 0 && out.MemoryBytes == 0 {
		return nil
	}
	return out
}

func serviceWorkloadDiagnosticLabel(rolloutCode commonv1.WorkloadDiagnosticCode, serviceMessage string) string {
	if label := enumLabel(rolloutCode.String(), "WORKLOAD_DIAGNOSTIC_CODE_"); label != "" && label != "unspecified" {
		return label
	}
	if workloaddiagnostic.AdmissionBlocked(serviceMessage) {
		return enumLabel(commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED.String(), "WORKLOAD_DIAGNOSTIC_CODE_")
	}
	return ""
}

func firstWorkloadDiagnosticCode(values ...commonv1.WorkloadDiagnosticCode) commonv1.WorkloadDiagnosticCode {
	for _, value := range values {
		if value != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
			return value
		}
	}
	return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
}

func NewReplicaDTO(replica *servicev1.ServiceReplica) ReplicaDTO {
	if replica == nil {
		return ReplicaDTO{}
	}
	return ReplicaDTO{
		ID:               replica.GetID(),
		ServiceID:        replica.GetServiceID(),
		NodeID:           replica.GetNodeID(),
		Attempt:          replica.GetAttempt(),
		Status:           enumLabel(replica.GetStatus().String(), "ALLOCATION_STATUS_"),
		Ready:            replica.GetReady(),
		Ended:            replica.GetEnded(),
		Outdated:         replica.GetOutdated(),
		Message:          replica.GetMessage(),
		ReadinessMessage: replica.GetReadinessMessage(),
		DiagnosticCode:   enumLabel(replica.GetDiagnosticCode().String(), "WORKLOAD_DIAGNOSTIC_CODE_"),
		LifecycleRetry:   newReplicaLifecycleRetryDTO(replica.GetLifecycleRetry()),
		CreatedAt:        formatTime(replica.GetCreatedAt()),
		UpdatedAt:        formatTime(replica.GetUpdatedAt()),
	}
}

func newReplicaLifecycleRetryDTO(retry *servicev1.ServiceReplicaLifecycleRetry) *ReplicaLifecycleRetryDTO {
	if retry == nil {
		return nil
	}
	return &ReplicaLifecycleRetryDTO{
		Reason:    enumLabel(retry.GetReason().String(), "SERVICE_REPLICA_LIFECYCLE_RETRY_REASON_"),
		Attempts:  retry.GetAttempts(),
		LastError: retry.GetLastError(),
		NextRunAt: formatTime(retry.GetNextRunAt()),
	}
}

func NewServiceEvent(event *servicev1.ServiceEvent) ServiceEvent {
	if event == nil {
		return ServiceEvent{}
	}
	return ServiceEvent{
		ID:             event.GetID(),
		ServiceID:      event.GetServiceID(),
		Type:           enumLabel(event.GetType().String(), "SERVICE_EVENT_TYPE_"),
		Phase:          enumLabel(event.GetPhase().String(), "SERVICE_ROLLOUT_PHASE_"),
		DiagnosticCode: enumLabel(event.GetDiagnosticCode().String(), "WORKLOAD_DIAGNOSTIC_CODE_"),
		Message:        event.GetMessage(),
		CreatedAt:      formatTime(event.GetCreatedAt()),
	}
}

func NewTunnelDTO(session *tunnelv1.TunnelSession) TunnelDTO {
	if session == nil {
		return TunnelDTO{}
	}
	return TunnelDTO{
		SessionID:       session.GetSessionID(),
		AllocationID:    session.GetAllocationID(),
		NodeID:          session.GetNodeID(),
		Status:          enumLabel(session.GetStatus().String(), "TUNNEL_SESSION_STATUS_"),
		RelayID:         session.GetRelayID(),
		BoundAddr:       session.GetBoundAddr(),
		ClientTarget:    firstNonEmpty(session.GetClientEdgeTarget(), session.GetEdgeTarget()),
		NodeTarget:      session.GetNodeEdgeTarget(),
		LocalTarget:     session.GetLocalTarget(),
		RemotePort:      session.GetRemotePort(),
		Reason:          session.GetReason(),
		BytesIn:         session.GetBytesIn(),
		BytesOut:        session.GetBytesOut(),
		CreatedAt:       formatTime(session.GetCreatedAt()),
		UpdatedAt:       formatTime(session.GetUpdatedAt()),
		ReadyAt:         formatTime(session.GetReadyAt()),
		ExpiresAt:       formatTime(session.GetExpiresAt()),
		LastPeerEventAt: formatTime(session.GetLastPeerEventAt()),
	}
}

func NewTunnelEvent(event *tunnelv1.TunnelSessionEvent) TunnelEvent {
	if event == nil {
		return TunnelEvent{}
	}
	return TunnelEvent{
		EventID:    event.GetEventID(),
		SessionID:  event.GetSessionID(),
		Type:       enumLabel(event.GetEventType().String(), "TUNNEL_SESSION_EVENT_TYPE_"),
		Status:     enumLabel(event.GetStatus().String(), "TUNNEL_SESSION_STATUS_"),
		ReasonCode: enumLabel(event.GetReasonCode().String(), "TUNNEL_SESSION_EVENT_REASON_CODE_"),
		Reason:     event.GetReason(),
		RelayID:    event.GetRelayID(),
		PeerKind:   enumLabel(event.GetPeerKind().String(), "TUNNEL_PEER_KIND_"),
		BoundAddr:  event.GetBoundAddr(),
		BytesIn:    event.GetBytesIn(),
		BytesOut:   event.GetBytesOut(),
		CreatedAt:  formatTime(event.GetCreatedAt()),
	}
}

func NewAllocationLifecycleRetryDTO(retry *adminv1.AllocationLifecycleRetry) AllocationLifecycleRetryDTO {
	if retry == nil {
		return AllocationLifecycleRetryDTO{}
	}
	return AllocationLifecycleRetryDTO{
		AllocationID:       retry.GetAllocationID(),
		OwnerID:            retry.GetOwnerID(),
		OwnerType:          enumLabel(retry.GetOwnerType().String(), "ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_"),
		EnvironmentID:      retry.GetEnvironmentID(),
		Reason:             enumLabel(retry.GetReason().String(), "ALLOCATION_LIFECYCLE_RETRY_REASON_"),
		NodeID:             retry.GetNodeID(),
		NodeTarget:         retry.GetNodeTarget(),
		Attempt:            retry.GetAttempt(),
		ReconcileAttempts:  retry.GetReconcileAttempts(),
		LastError:          retry.GetLastError(),
		NextRunAt:          formatTime(retry.GetNextRunAt()),
		CreatedAt:          formatTime(retry.GetCreatedAt()),
		UpdatedAt:          formatTime(retry.GetUpdatedAt()),
		AgeSeconds:         retry.GetAgeSeconds(),
		Due:                retry.GetDue(),
		Clearable:          retry.GetClearable(),
		ClearBlockedReason: retry.GetClearBlockedReason(),
	}
}

func NewAdminAuditEventDTO(event *adminv1.AdminAuditEvent) AdminAuditEventDTO {
	if event == nil {
		return AdminAuditEventDTO{}
	}
	return AdminAuditEventDTO{
		EventID:        event.GetEventID(),
		Operation:      enumLabel(event.GetOperation().String(), "ADMIN_AUDIT_OPERATION_"),
		TargetType:     enumLabel(event.GetTargetType().String(), "ADMIN_AUDIT_TARGET_TYPE_"),
		TargetID:       event.GetTargetID(),
		OperatorReason: event.GetOperatorReason(),
		CreatedAt:      formatTime(event.GetCreatedAt()),
	}
}

func NewReliabilitySummary(health *adminv1.AdminReliabilityHealth) *ReliabilitySummary {
	if health == nil {
		return nil
	}
	signals := make([]ReliabilitySignalSummary, 0, len(health.GetSignals()))
	for _, signal := range health.GetSignals() {
		signals = append(signals, ReliabilitySignalSummary{
			Code:    enumLabel(signal.GetCode().String(), "ADMIN_RELIABILITY_SIGNAL_CODE_"),
			Message: signal.GetMessage(),
		})
	}
	issues := make([]ConsistencyIssueSummary, 0)
	var consistencyStatus string
	var consistencyIssues int64
	if consistency := health.GetConsistency(); consistency != nil {
		consistencyStatus = enumLabel(consistency.GetStatus().String(), "CONSISTENCY_STATUS_")
		if counts := consistency.GetCounts(); counts != nil {
			consistencyIssues = counts.GetIssues()
		}
		issues = make([]ConsistencyIssueSummary, 0, len(consistency.GetIssues()))
		for _, issue := range consistency.GetIssues() {
			issues = append(issues, ConsistencyIssueSummary{
				Code:             enumLabel(issue.GetCode().String(), "CONSISTENCY_ISSUE_CODE_"),
				Severity:         enumLabel(issue.GetSeverity().String(), "CONSISTENCY_ISSUE_SEVERITY_"),
				AllocationID:     issue.GetAllocationID(),
				OwnerType:        issue.GetOwnerType(),
				OwnerID:          issue.GetOwnerID(),
				NodeID:           issue.GetNodeID(),
				Status:           issue.GetStatus(),
				Detail:           issue.GetDetail(),
				RepairOwner:      enumLabel(issue.GetRepairOwner().String(), "CONSISTENCY_REPAIR_OWNER_"),
				RepairAction:     enumLabel(issue.GetRepairAction().String(), "CONSISTENCY_REPAIR_ACTION_"),
				RepairTargetType: enumLabel(issue.GetRepairTargetType().String(), "CONSISTENCY_REPAIR_TARGET_TYPE_"),
				RepairTargetID:   issue.GetRepairTargetID(),
				AutomaticRepair:  issue.GetAutomaticRepair(),
			})
		}
	}
	return &ReliabilitySummary{
		Status:                        enumLabel(health.GetStatus().String(), "ADMIN_RELIABILITY_STATUS_"),
		ConsistencyStatus:             consistencyStatus,
		ConsistencyIssues:             consistencyIssues,
		AllocationLifecycleRetries:    health.GetAllocationLifecycleRetries(),
		DueAllocationLifecycleRetries: health.GetDueAllocationLifecycleRetries(),
		ReconcileUnhealthyComponents:  health.GetReconcileUnhealthyComponents(),
		NodeFleet:                     newNodeFleetSummary(health.GetNodeFleetHealth()),
		Signals:                       signals,
		Issues:                        issues,
	}
}

func newNodeFleetSummary(health *adminv1.AdminNodeFleetHealth) *NodeFleetSummary {
	if health == nil {
		return nil
	}
	return &NodeFleetSummary{Unavailable: health.GetUnavailable(), Error: health.GetError(), ActiveNodes: health.GetActiveNodes(), ReadyNodes: health.GetReadyNodes(), StaleHeartbeatNodes: health.GetStaleHeartbeatNodes(), StaleSummaryNodes: health.GetStaleSummaryNodes(), NotReadyNodes: health.GetNotReadyNodes()}
}

func NewQuotaDTO(quota *quotav1.NamespaceQuota) QuotaDTO {
	if quota == nil {
		return QuotaDTO{}
	}
	cpuLimit := optionalInt64(quota.GetCpuMilliLimit())
	memoryLimit := optionalInt64(quota.GetMemoryBytesLimit())
	return QuotaDTO{
		Namespace:            quota.GetNamespace(),
		CPUMilliLimit:        cpuLimit,
		MemoryBytesLimit:     memoryLimit,
		ReservedCPUMilli:     quota.GetReservedCpuMilli(),
		ReservedMemoryBytes:  quota.GetReservedMemoryBytes(),
		AvailableCPUMilli:    optionalInt64(quota.GetAvailableCpuMilli()),
		AvailableMemoryBytes: optionalInt64(quota.GetAvailableMemoryBytes()),
		CPUUsagePercent:      quotaUsagePercent(quota.GetReservedCpuMilli(), cpuLimit),
		MemoryUsagePercent:   quotaUsagePercent(quota.GetReservedMemoryBytes(), memoryLimit),
		Version:              quota.GetVersion(),
		UpdatedAt:            formatTime(quota.GetUpdatedAt()),
	}
}

func NewQuotaEventDTO(event *quotav1.NamespaceQuotaEvent) QuotaEventDTO {
	if event == nil {
		return QuotaEventDTO{}
	}
	return QuotaEventDTO{
		ID:                   event.GetID(),
		Namespace:            event.GetNamespace(),
		Type:                 enumLabel(event.GetType().String(), "NAMESPACE_QUOTA_EVENT_TYPE_"),
		WorkloadType:         enumLabel(event.GetWorkloadType().String(), "NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_"),
		WorkloadID:           event.GetWorkloadID(),
		EnvironmentID:        event.GetEnvironmentID(),
		Reason:               enumLabel(event.GetReason().String(), "NAMESPACE_QUOTA_EVENT_REASON_"),
		RequestedCPUMilli:    event.GetRequestedCpuMilli(),
		ReservedCPUMilli:     event.GetReservedCpuMilli(),
		CPUMilliLimit:        optionalInt64(event.GetCpuMilliLimit()),
		AvailableCPUMilli:    optionalInt64(event.GetAvailableCpuMilli()),
		RequestedMemoryBytes: event.GetRequestedMemoryBytes(),
		ReservedMemoryBytes:  event.GetReservedMemoryBytes(),
		MemoryBytesLimit:     optionalInt64(event.GetMemoryBytesLimit()),
		AvailableMemoryBytes: optionalInt64(event.GetAvailableMemoryBytes()),
		Message:              event.GetMessage(),
		CreatedAt:            formatTime(event.GetCreatedAt()),
	}
}

func quotaUsagePercent(reserved int64, limit *int64) *int64 {
	if limit == nil || *limit <= 0 {
		return nil
	}
	if reserved <= 0 {
		return ptr[int64](0)
	}
	percent := min((reserved*100) / *limit, 100)
	return ptr(percent)
}

func optionalInt64(value *wrapperspb.Int64Value) *int64 {
	if value == nil {
		return nil
	}
	out := value.GetValue()
	return &out
}

func enumLabel(value, prefix string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimPrefix(value, prefix)
	return strings.ToLower(value)
}

func formatTime(ts *timestamppb.Timestamp) string {
	if ts == nil || !ts.IsValid() {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ptr[T any](value T) *T {
	return &value
}
