package output

import (
	"strings"
	"testing"

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
		Attempt:       2,
		Status:        runv1.RunStatus_RUN_STATUS_RUNNING,
		Message:       "starting workload",
	})
	out := b.String()
	for _, want := range []string{"ID: run-1", "Namespace: default", "Status: running", "Allocation ID: alloc-1", "Attempt: 2", "Message: starting workload"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderRunHighlightsAdmissionBlockedMessage(t *testing.T) {
	var b strings.Builder
	RenderRun(&b, &runv1.Run{
		ID:            "run-quota",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        runv1.RunStatus_RUN_STATUS_FAILED,
		Message:       "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a cpu requested_milli=500 reserved_milli=0 limit_milli=100 available_milli=100",
	})
	out := b.String()
	for _, want := range []string{
		"Status: failed",
		"Diagnostic: admission-blocked",
		"Admission: namespace quota exceeded",
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
		Attempt:   3,
		CreatedAt: timestamppb.Now(),
		UpdatedAt: timestamppb.Now(),
	}})
	out := b.String()
	for _, want := range []string{"ID", "NAMESPACE", "AGE", "run-1", "team-a", "failed", "3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderRunClassifiesPlainNoEligibleNodeAsSelection(t *testing.T) {
	var b strings.Builder
	RenderRun(&b, &runv1.Run{
		ID:            "run-placement",
		Namespace:     "team-a",
		EnvironmentID: "env-1",
		Status:        runv1.RunStatus_RUN_STATUS_FAILED,
		Message:       "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
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
	if strings.Contains(out, "Admission:") {
		t.Fatalf("output %q unexpectedly contains admission summary", out)
	}
}
