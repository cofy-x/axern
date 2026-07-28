package workloadkernel

import (
	"strings"

	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func ClassifyDiagnostic(status commonv1.AllocationStatus, message string) commonv1.WorkloadDiagnosticCode {
	message = strings.ToLower(strings.TrimSpace(message))
	switch {
	case resourcekernel.MessageIndicatesAdmissionBlocked(message):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED
	case containsDiagnosticToken(message,
		"liveness probe failed",
		"liveness probe returned",
		"liveness probe tcp connect",
		"liveness probe execution requires",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_LIVENESS_PROBE_FAILED
	case containsDiagnosticToken(message,
		"config.secret_env",
		"config.secret_files",
		"references secret",
		"resolve secret env",
		"resolve secret file",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR
	case containsDiagnosticToken(message,
		"authentication failed or access was denied",
		"check the referenced docker-config-json secret",
		"registry credential",
		"docker-config-json",
		"unauthorized",
		"denied",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR
	case containsDiagnosticToken(message,
		"resolve image ref",
		"image or tag was not found",
		"manifest unknown",
		"manifest_unknown",
		"name unknown",
		"name_unknown",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR
	case containsDiagnosticToken(message,
		"service volume topology",
		"volume topology",
		"storage topology",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_STORAGE_TOPOLOGY_UNSATISFIED
	case containsDiagnosticToken(message,
		"volume spec conflict",
		"different resolved volume",
		"already exists with a different resolved volume",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_SPEC_CONFLICT
	case containsDiagnosticToken(message,
		"storage reserve failed",
		"volume binding reserve",
		"reserve volume binding",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_STORAGE_RESERVE_ERROR
	case containsDiagnosticToken(message,
		"volume release failed",
		"release volume",
		"unpublish volume",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_RELEASE_ERROR
	case containsDiagnosticToken(message,
		"volume publish failed",
		"publish volume",
		"volumed",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_PUBLISH_ERROR
	case containsDiagnosticToken(message,
		"no eligible node",
		"no candidates",
		"candidate selection",
		"placement",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR
	case containsDiagnosticToken(message,
		"node create",
		"runtime create",
		"node start failed",
		"container start",
		"failed to validate mount targets",
		"readonly image rootfs",
	):
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR
	case status == commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED:
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED
	case status == commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED && message != "":
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR
	default:
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
	}
}

func containsDiagnosticToken(message string, tokens ...string) bool {
	if message == "" {
		return false
	}
	for _, token := range tokens {
		if strings.Contains(message, strings.ToLower(strings.TrimSpace(token))) {
			return true
		}
	}
	return false
}
