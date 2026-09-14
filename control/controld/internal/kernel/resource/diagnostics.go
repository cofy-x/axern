package resource

const AdmissionErrorDomain = "axern.control.resource_admission"

type AdmissionRejectionReason string

const (
	AdmissionRejectionNamespaceQuotaExceeded AdmissionRejectionReason = "NAMESPACE_QUOTA_EXCEEDED"
	AdmissionRejectionNodeCapacity           AdmissionRejectionReason = "NODE_CAPACITY_EXHAUSTED"
	AdmissionRejectionPlacementCapacity      AdmissionRejectionReason = "PLACEMENT_CAPACITY_EXHAUSTED"
	AdmissionRejectionNodeSelection          AdmissionRejectionReason = "NODE_SELECTION_ERROR"
)

type AdmissionDiagnosticCode string

const (
	AdmissionDiagnosticUnspecified            AdmissionDiagnosticCode = ""
	AdmissionDiagnosticNamespaceQuotaExceeded AdmissionDiagnosticCode = "namespace_quota_exceeded"
	AdmissionDiagnosticNodeCapacity           AdmissionDiagnosticCode = "node_capacity_exhausted"
	AdmissionDiagnosticPlacementCapacity      AdmissionDiagnosticCode = "placement_capacity_exhausted"
	AdmissionDiagnosticNodeSelection          AdmissionDiagnosticCode = "node_selection_error"
)

type QuotaEventType string

const (
	QuotaEventTypeAdmissionRejected QuotaEventType = "admission_rejected"
)

type QuotaEventReason string

const (
	QuotaEventReasonInsufficientCPU              QuotaEventReason = "insufficient_cpu"
	QuotaEventReasonInsufficientMemory           QuotaEventReason = "insufficient_memory"
	QuotaEventReasonInsufficientCPUMemory        QuotaEventReason = "insufficient_cpu_memory"
	QuotaEventReasonInsufficientEphemeralStorage QuotaEventReason = "insufficient_ephemeral_storage"
	QuotaEventReasonExceeded                     QuotaEventReason = "exceeded"
)

func AdmissionDiagnosticForReason(reason AdmissionRejectionReason) AdmissionDiagnosticCode {
	switch reason {
	case AdmissionRejectionNamespaceQuotaExceeded:
		return AdmissionDiagnosticNamespaceQuotaExceeded
	case AdmissionRejectionNodeCapacity:
		return AdmissionDiagnosticNodeCapacity
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
		AdmissionRejectionNodeCapacity,
		AdmissionRejectionPlacementCapacity:
		return true
	default:
		return false
	}
}

func QuotaEventReasonForEvaluation(evaluation QuotaEvaluation) QuotaEventReason {
	cpu := evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits
	memory := evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits
	ephemeralStorage := evaluation.EphemeralStorage.Requested > 0 && !evaluation.EphemeralStorage.Fits
	switch {
	case ephemeralStorage && !cpu && !memory:
		return QuotaEventReasonInsufficientEphemeralStorage
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
