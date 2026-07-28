package workloadkernel

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestClassifyDiagnostic(t *testing.T) {
	tests := []struct {
		name    string
		status  commonv1.AllocationStatus
		message string
		want    commonv1.WorkloadDiagnosticCode
	}{
		{
			name:    "secret projection",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: `config.secret_env "TOKEN" references secret "sec-missing"`,
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR,
		},
		{
			name:    "registry auth",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "resolve image ref ghcr.io/acme/app:latest: authentication failed or access was denied; check the referenced docker-config-json secret and repository permissions",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_REGISTRY_AUTH_ERROR,
		},
		{
			name:    "image resolution",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "resolve image ref ghcr.io/acme/app:missing: image or tag was not found",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_IMAGE_RESOLUTION_ERROR,
		},
		{
			name:    "storage topology",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "service volume topology unsatisfied: no placement candidates satisfy required volume topology",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_STORAGE_TOPOLOGY_UNSATISFIED,
		},
		{
			name:    "storage reserve",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "storage reserve failed: volume binding reserve requires claim, class, and mount",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_STORAGE_RESERVE_ERROR,
		},
		{
			name:    "volume spec conflict",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "storage reserve failed: volume binding \"alloc-a/data\" already exists with a different resolved volume",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_SPEC_CONFLICT,
		},
		{
			name:    "volume publish",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "volume publish failed: volumed: volume does not support runtime class \"runsc\"",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_PUBLISH_ERROR,
		},
		{
			name:    "volume release",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "volume release failed: unpublish volume alloc-a/data: input/output error",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_VOLUME_RELEASE_ERROR,
		},
		{
			name:    "quota admission blocked",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a cpu requested_milli=500 reserved_milli=0 limit_milli=100 available_milli=100",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED,
		},
		{
			name:    "node reservation blocked",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "no node has remaining reservation capacity: node_id=node-a cpu requested_milli=300 reserved_milli=1800 effective_allocatable_milli=2000 available_milli=200",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED,
		},
		{
			name:    "retriable node create",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND,
			message: "node create temporarily unavailable",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR,
		},
		{
			name:    "node resource exhausted",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED,
			message: "rpc error: code = ResourceExhausted desc = allocate resource interface failed: resource exhausted",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR,
		},
		{
			name:    "process exited",
			status:  commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
			message: "process exited with status 17",
			want:    commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyDiagnostic(tt.status, tt.message); got != tt.want {
				t.Fatalf("ClassifyDiagnostic(%v, %q) = %v, want %v", tt.status, tt.message, got, tt.want)
			}
		})
	}
}
