package output

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderServiceReplicaTableIncludesOutdatedAndEnded(t *testing.T) {
	var b strings.Builder
	RenderServiceReplicaTable(&b, []*servicev1.ServiceReplica{{
		ID:               "alloc-1",
		Status:           commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		NodeID:           "node-1",
		Attempt:          2,
		Ready:            false,
		ReadinessMessage: "waiting for readiness probe",
		Ended:            true,
		Outdated:         true,
		DiagnosticCode:   commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_PROCESS_EXITED,
		UpdatedAt:        timestamppb.Now(),
		Message:          "container exited with status 1 after rollout",
	}})
	out := b.String()
	for _, want := range []string{"alloc-1", "exited", "true", "process-exited", "waiting for readiness probe"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceReplicaTableOmitsEmptyDiagnosticColumns(t *testing.T) {
	var b strings.Builder
	RenderServiceReplicaTable(&b, []*servicev1.ServiceReplica{{
		ID:        "alloc-1",
		Status:    commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
		NodeID:    "node-1",
		Attempt:   1,
		Ready:     true,
		UpdatedAt: timestamppb.Now(),
	}})
	out := b.String()
	for _, want := range []string{"ID", "STATUS", "READY", "NODE", "ATTEMPT", "UPDATED", "alloc-1", "running"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
	for _, unexpected := range []string{"ENDED", "OUTDATED", "DIAGNOSTIC", "READINESS", "MESSAGE"} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("output %q unexpectedly contains %q", out, unexpected)
		}
	}
}
