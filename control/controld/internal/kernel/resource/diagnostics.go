package resource

import "strings"

const AdmissionErrorDomain = "axern.control.resource_admission"

type AdmissionRejectionReason string

const (
	AdmissionRejectionNamespaceQuotaExceeded  AdmissionRejectionReason = "NAMESPACE_QUOTA_EXCEEDED"
	AdmissionRejectionNodeReservationCapacity AdmissionRejectionReason = "NODE_RESERVATION_CAPACITY_EXHAUSTED"
	AdmissionRejectionPlacementCapacity       AdmissionRejectionReason = "PLACEMENT_CAPACITY_EXHAUSTED"
	AdmissionRejectionNodeSelection           AdmissionRejectionReason = "NODE_SELECTION_ERROR"
)

type AdmissionDiagnosticCode string

const (
	AdmissionDiagnosticUnspecified             AdmissionDiagnosticCode = ""
	AdmissionDiagnosticNamespaceQuotaExceeded  AdmissionDiagnosticCode = "namespace_quota_exceeded"
	AdmissionDiagnosticNodeReservationCapacity AdmissionDiagnosticCode = "node_reservation_capacity_exhausted"
	AdmissionDiagnosticPlacementCapacity       AdmissionDiagnosticCode = "placement_capacity_exhausted"
	AdmissionDiagnosticNodeSelection           AdmissionDiagnosticCode = "node_selection_error"
)

type QuotaEventType string

const (
	QuotaEventTypeAdmissionRejected QuotaEventType = "admission_rejected"
)

type QuotaEventWorkloadType string

const (
	QuotaEventWorkloadRun     QuotaEventWorkloadType = "run"
	QuotaEventWorkloadService QuotaEventWorkloadType = "service"
)

type QuotaEventReason string

const (
	QuotaEventReasonInsufficientCPU       QuotaEventReason = "insufficient_cpu"
	QuotaEventReasonInsufficientMemory    QuotaEventReason = "insufficient_memory"
	QuotaEventReasonInsufficientCPUMemory QuotaEventReason = "insufficient_cpu_memory"
	QuotaEventReasonExceeded              QuotaEventReason = "exceeded"
)

func AdmissionDiagnosticForReason(reason AdmissionRejectionReason) AdmissionDiagnosticCode {
	switch reason {
	case AdmissionRejectionNamespaceQuotaExceeded:
		return AdmissionDiagnosticNamespaceQuotaExceeded
	case AdmissionRejectionNodeReservationCapacity:
		return AdmissionDiagnosticNodeReservationCapacity
	case AdmissionRejectionPlacementCapacity:
		return AdmissionDiagnosticPlacementCapacity
	case AdmissionRejectionNodeSelection:
		return AdmissionDiagnosticNodeSelection
	default:
		return AdmissionDiagnosticUnspecified
	}
}

func AdmissionReasonBlocksCapacity(reason AdmissionRejectionReason) bool {
	switch reason {
	case AdmissionRejectionNamespaceQuotaExceeded,
		AdmissionRejectionNodeReservationCapacity,
		AdmissionRejectionPlacementCapacity:
		return true
	default:
		return false
	}
}

func QuotaEventReasonForEvaluation(evaluation QuotaEvaluation) QuotaEventReason {
	cpu := evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits
	memory := evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits
	switch {
	case cpu && memory:
		return QuotaEventReasonInsufficientCPUMemory
	case cpu:
		return QuotaEventReasonInsufficientCPU
	case memory:
		return QuotaEventReasonInsufficientMemory
	default:
		return QuotaEventReasonExceeded
	}
}

func MessageIndicatesAdmissionBlocked(message string) bool {
	message = normalizeDiagnosticMessage(message)
	if message == "" {
		return false
	}
	if strings.Contains(message, "namespace quota exceeded") ||
		strings.Contains(message, "no node has remaining reservation capacity") {
		return true
	}
	return MessageIndicatesCapacityBlock(message)
}

func MessageIndicatesCapacityBlock(message string) bool {
	message = normalizeDiagnosticMessage(message)
	return strings.Contains(message, "insufficient_cpu") ||
		strings.Contains(message, "insufficient_memory") ||
		strings.Contains(message, "effective_allocatable")
}

func normalizeDiagnosticMessage(message string) string {
	return strings.ToLower(strings.TrimSpace(message))
}
