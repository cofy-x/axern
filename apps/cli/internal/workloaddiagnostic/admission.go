package workloaddiagnostic

import "strings"

const (
	DiagnosticAdmissionBlocked   = "admission-blocked"
	DiagnosticNodeSelectionError = "node-selection-error"

	tokenNamespaceQuotaExceeded       = "namespace quota exceeded"
	tokenNoNodeReservationCapacity    = "no node has remaining reservation capacity"
	tokenNoEligibleNode               = "no eligible node"
	tokenResourceExhausted            = "resource exhausted"
	tokenInsufficientCPU              = "insufficient_cpu"
	tokenInsufficientMemory           = "insufficient_memory"
	tokenEffectiveAllocatableCapacity = "effective_allocatable"
)

func DiagnosticCode(message string) string {
	message = normalizeMessage(message)
	switch {
	case AdmissionBlocked(message):
		return DiagnosticAdmissionBlocked
	case strings.Contains(message, tokenNoEligibleNode):
		return DiagnosticNodeSelectionError
	default:
		return ""
	}
}

func AdmissionBlocked(message string) bool {
	message = normalizeMessage(message)
	if message == "" {
		return false
	}
	if strings.Contains(message, tokenNamespaceQuotaExceeded) ||
		strings.Contains(message, tokenNoNodeReservationCapacity) ||
		strings.Contains(message, tokenResourceExhausted) {
		return true
	}
	return containsCapacityToken(message)
}

func AdmissionBlockedSummary(message string) string {
	message = normalizeMessage(message)
	switch {
	case strings.Contains(message, tokenNamespaceQuotaExceeded):
		return "namespace quota exceeded"
	case strings.Contains(message, tokenNoNodeReservationCapacity):
		return "node reservation capacity exhausted"
	case strings.Contains(message, tokenInsufficientCPU) && strings.Contains(message, tokenInsufficientMemory):
		return "node CPU and memory capacity exhausted"
	case strings.Contains(message, tokenInsufficientCPU):
		return "node CPU capacity exhausted"
	case strings.Contains(message, tokenInsufficientMemory):
		return "node memory capacity exhausted"
	case strings.Contains(message, tokenEffectiveAllocatableCapacity):
		return "node reservation capacity exhausted"
	case strings.Contains(message, tokenResourceExhausted):
		return "resource exhausted"
	default:
		return ""
	}
}

func normalizeMessage(message string) string {
	return strings.ToLower(strings.TrimSpace(message))
}

func containsCapacityToken(message string) bool {
	return strings.Contains(message, tokenInsufficientCPU) ||
		strings.Contains(message, tokenInsufficientMemory) ||
		strings.Contains(message, tokenEffectiveAllocatableCapacity)
}
