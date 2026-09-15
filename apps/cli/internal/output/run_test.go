package output

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderRun(t *testing.T) {
	var b strings.Builder
	RenderRun(&b, &runv1.Run{
		ID:            "run-1",
		Namespace:     "default",
		EnvironmentID: "env-1",
		AllocationID:  "alloc-1",
		Status:        runv1.RunStatus_RUN_STATUS_RUNNING,
		Message:       "starting workload",
	})
	out := b.String()
	for _, want := range []string{"ID: run-1", "Namespace: default", "Status: running", "Allocation ID: alloc-1", "Message: starting workload"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderRunDisplaysTypedAdmissionDiagnostic(t *testing.T) {
	var b strings.Builder
	RenderRun(&b, &runv1.Run{
		ID:             "run-quota",
		Namespace:      "team-a",
		EnvironmentID:  "env-1",
		Status:         runv1.RunStatus_RUN_STATUS_FAILED,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED,
		Message:        "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a cpu requested_milli=500 used_milli=0 limit_milli=100 available_milli=100",
	})
	out := b.String()
	for _, want := range []string{
		"Status: failed",
		"Diagnostic: admission-blocked",
		"Message: rpc error: code = ResourceExhausted desc = namespace quota exceeded",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderRunTable(t *testing.T) {
	var b strings.Builder
	RenderRunTable(&b, []*runv1.Run{{
		ID:        "run-1",
		Namespace: "team-a",
		Status:    runv1.RunStatus_RUN_STATUS_FAILED,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
	}})
	out := b.String()
	for _, want := range []string{"ID", "NAMESPACE", "AGE", "run-1", "team-a", "failed"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderRunDisplaysTypedNodeSelectionDiagnostic(t *testing.T) {
	var b strings.Builder
	RenderRun(&b, &runv1.Run{
		ID:             "run-placement",
		Namespace:      "team-a",
		EnvironmentID:  "env-1",
		Status:         runv1.RunStatus_RUN_STATUS_FAILED,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_NODE_SELECTION_ERROR,
		Message:        "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
	})
	out := b.String()
	for _, want := range []string{
		"Diagnostic: node-selection-error",
		"Message: no eligible node",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}
