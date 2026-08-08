package servicekernel

import (
	"fmt"
	"strings"

	workloadkernel "github.com/cofy-x/axern/control/controld/internal/kernel/workload"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type RolloutState struct {
	desired        int
	maxSurge       int
	maxUnavailable int
	totalReady     int
	updatedReady   int
	allocations    []*AllocationRecord
	outdated       []*AllocationRecord
}

func buildRolloutStatus(service *servicev1.Service, allocations []*AllocationRecord) *servicev1.ServiceRolloutStatus {
	if service == nil {
		return nil
	}
	rollout := newRolloutState(service, allocations)
	if !rollout.inProgress() {
		return nil
	}
	diagnosticMessage := strings.TrimSpace(service.GetMessage())
	diagnosticCode := service.GetDiagnosticCode()
	if diagnosticCode == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		diagnosticCode = workloadkernel.ClassifyDiagnostic(rolloutDiagnosticAllocationStatus(service.GetStatus()), diagnosticMessage)
	}
	return &servicev1.ServiceRolloutStatus{
		InProgress:           true,
		CurrentReplicas:      int32(len(rollout.allocations)),
		UpdatedReadyReplicas: int32(rollout.updatedReady),
		OutdatedReplicas:     int32(len(rollout.outdated)),
		Phase:                rollout.phase(service, diagnosticCode),
		DiagnosticCode:       diagnosticCode,
		DiagnosticMessage:    diagnosticMessage,
	}
}

func rolloutDiagnosticAllocationStatus(status servicev1.ServiceStatus) commonv1.AllocationStatus {
	switch status {
	case servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED, servicev1.ServiceStatus_SERVICE_STATUS_FAILED:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED
	default:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED
	}
}

func BuildRolloutStatus(service *servicev1.Service, allocations []*AllocationRecord) *servicev1.ServiceRolloutStatus {
	return buildRolloutStatus(service, allocations)
}

func outdatedAllocations(allocations []*AllocationRecord, service *servicev1.Service) []*AllocationRecord {
	out := make([]*AllocationRecord, 0, len(allocations))
	for _, alloc := range allocations {
		if alloc == nil || alloc.Status == commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING {
			continue
		}
		if allocationOutdated(alloc.DesiredSpecDigest, service) {
			out = append(out, alloc)
		}
	}
	return out
}

func countReadyCurrentConfig(allocations []*AllocationRecord, service *servicev1.Service) int {
	total := 0
	for _, alloc := range allocations {
		if alloc == nil {
			continue
		}
		if alloc.Status == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING && alloc.Ready && allocationMatchesDesired(alloc.DesiredSpecDigest, service) {
			total++
		}
	}
	return total
}

func countReadyAllocations(allocations []*AllocationRecord) int {
	total := 0
	for _, alloc := range allocations {
		if alloc != nil && alloc.Status == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING && alloc.Ready {
			total++
		}
	}
	return total
}

func newRolloutState(service *servicev1.Service, allocations []*AllocationRecord) RolloutState {
	policy := normalizeRolloutPolicy(service.GetRolloutPolicy())
	return RolloutState{
		desired:        int(service.GetReplicas()),
		maxSurge:       int(policy.GetMaxSurge()),
		maxUnavailable: int(policy.GetMaxUnavailable()),
		totalReady:     countReadyAllocations(allocations),
		updatedReady:   countReadyCurrentConfig(allocations, service),
		allocations:    allocations,
		outdated:       outdatedAllocations(allocations, service),
	}
}

func NewRolloutState(service *servicev1.Service, allocations []*AllocationRecord) RolloutState {
	return newRolloutState(service, allocations)
}

func (r RolloutState) inProgress() bool {
	return r.desired > 0 && len(r.outdated) > 0
}

func (r RolloutState) InProgress() bool {
	return r.inProgress()
}

func (r RolloutState) targetSize() int {
	return r.desired + r.maxSurge
}

func (r RolloutState) canAdmitReplacement() bool {
	return len(r.allocations) < r.targetSize()
}

func (r RolloutState) CanAdmitReplacement() bool {
	return r.canAdmitReplacement()
}

func (r RolloutState) removableOutdatedAllocation() *AllocationRecord {
	for _, alloc := range r.outdated {
		if alloc == nil {
			continue
		}
		if alloc.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
			return alloc
		}
	}
	minReadyAfterRemoval := r.desired - r.maxUnavailable
	if minReadyAfterRemoval < 0 {
		minReadyAfterRemoval = 0
	}
	if r.totalReady-1 < minReadyAfterRemoval {
		return nil
	}
	for _, alloc := range r.outdated {
		if alloc != nil && alloc.Status == commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
			return alloc
		}
	}
	return nil
}

func (r RolloutState) RemovableOutdatedAllocation() *AllocationRecord {
	return r.removableOutdatedAllocation()
}

func (r RolloutState) progressMessage() string {
	return fmt.Sprintf(
		"rolling update in progress: %d outdated replica(s), %d updated ready replica(s)",
		len(r.outdated),
		r.updatedReady,
	)
}

func (r RolloutState) ProgressMessage() string {
	return r.progressMessage()
}

func (r RolloutState) phase(service *servicev1.Service, diagnosticCode commonv1.WorkloadDiagnosticCode) servicev1.ServiceRolloutPhase {
	if service != nil &&
		service.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED &&
		diagnosticCode != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED
	}
	if r.canAdmitReplacement() {
		return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_ADMITTING_REPLACEMENT
	}
	if r.updatedReady < r.desired {
		return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY
	}
	if len(r.outdated) > 0 {
		return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_DRAINING_OUTDATED
	}
	return servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_UNSPECIFIED
}
