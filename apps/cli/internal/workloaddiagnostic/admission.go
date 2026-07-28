package workloaddiagnostic

import "strings"

const (
	DiagnosticAdmissionBlocked           = "admission-blocked"
	DiagnosticNodeSelectionError         = "node-selection-error"
	DiagnosticStorageTopologyUnsatisfied = "storage-topology-unsatisfied"
	DiagnosticStorageReserveError        = "storage-reserve-error"
	DiagnosticVolumePublishError         = "volume-publish-error"
	DiagnosticVolumeReleaseError         = "volume-release-error"
	DiagnosticVolumeSpecConflict         = "volume-spec-conflict"

	tokenNamespaceQuotaExceeded       = "namespace quota exceeded"
	tokenNoNodeReservationCapacity    = "no node has remaining reservation capacity"
	tokenNoEligibleNode               = "no eligible node"
	tokenResourceExhausted            = "resource exhausted"
	tokenInsufficientCPU              = "insufficient_cpu"
	tokenInsufficientMemory           = "insufficient_memory"
	tokenEffectiveAllocatableCapacity = "effective_allocatable"
	tokenServiceVolumeTopology        = "service volume topology"
	tokenVolumeTopology               = "volume topology"
	tokenStorageTopology              = "storage topology"
	tokenStorageReserveFailed         = "storage reserve failed"
	tokenVolumeSpecConflict           = "volume spec conflict"
	tokenDifferentResolvedVolume      = "different resolved volume"
	tokenVolumeBindingReserve         = "volume binding reserve"
	tokenReserveVolumeBinding         = "reserve volume binding"
	tokenVolumeReleaseFailed          = "volume release failed"
	tokenReleaseVolume                = "release volume"
	tokenUnpublishVolume              = "unpublish volume"
	tokenVolumePublishFailed          = "volume publish failed"
	tokenPublishVolume                = "publish volume"
	tokenVolumed                      = "volumed"
)

func DiagnosticCode(message string) string {
	message = normalizeMessage(message)
	switch {
	case AdmissionBlocked(message):
		return DiagnosticAdmissionBlocked
	case StorageTopologyUnsatisfied(message):
		return DiagnosticStorageTopologyUnsatisfied
	case VolumeSpecConflict(message):
		return DiagnosticVolumeSpecConflict
	case StorageReserveError(message):
		return DiagnosticStorageReserveError
	case VolumeReleaseError(message):
		return DiagnosticVolumeReleaseError
	case VolumePublishError(message):
		return DiagnosticVolumePublishError
	case strings.Contains(message, tokenNoEligibleNode):
		return DiagnosticNodeSelectionError
	default:
		return ""
	}
}

func VolumeSpecConflict(message string) bool {
	message = normalizeMessage(message)
	return strings.Contains(message, tokenVolumeSpecConflict) ||
		strings.Contains(message, tokenDifferentResolvedVolume)
}

func StorageTopologyUnsatisfied(message string) bool {
	message = normalizeMessage(message)
	return strings.Contains(message, tokenServiceVolumeTopology) ||
		strings.Contains(message, tokenVolumeTopology) ||
		strings.Contains(message, tokenStorageTopology)
}

func StorageReserveError(message string) bool {
	message = normalizeMessage(message)
	return strings.Contains(message, tokenStorageReserveFailed) ||
		strings.Contains(message, tokenVolumeBindingReserve) ||
		strings.Contains(message, tokenReserveVolumeBinding)
}

func VolumeReleaseError(message string) bool {
	message = normalizeMessage(message)
	return strings.Contains(message, tokenVolumeReleaseFailed) ||
		strings.Contains(message, tokenReleaseVolume) ||
		strings.Contains(message, tokenUnpublishVolume)
}

func VolumePublishError(message string) bool {
	message = normalizeMessage(message)
	return strings.Contains(message, tokenVolumePublishFailed) ||
		strings.Contains(message, tokenPublishVolume) ||
		strings.Contains(message, tokenVolumed)
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
