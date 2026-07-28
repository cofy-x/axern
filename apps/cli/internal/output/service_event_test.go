package output

import (
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderServiceLatestEvent(t *testing.T) {
	var b strings.Builder
	RenderServiceLatestEvent(&b, &servicev1.ServiceEvent{
		ID:             "svcevt-1",
		ServiceID:      "svc-1",
		ReplicaID:      "alloc-2",
		Type:           servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_BLOCKED,
		Phase:          servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR,
		Message:        "config.secret_env TOKEN references secret sec-missing",
	})
	out := b.String()
	for _, want := range []string{"Latest Event:", "replacement-blocked", "blocked", "secret-projection-error", "alloc-2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}

func TestRenderServiceEventTable(t *testing.T) {
	var b strings.Builder
	RenderServiceEventTable(&b, []*servicev1.ServiceEvent{{
		ID:             "svcevt-1",
		ServiceID:      "svc-1",
		ReplicaID:      "alloc-1",
		Type:           servicev1.ServiceEventType_SERVICE_EVENT_TYPE_REPLACEMENT_RUNNING,
		Phase:          servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_WAITING_FOR_UPDATED_READY,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		Message:        "replacement replica is running",
		CreatedAt:      timestamppb.Now(),
	}})
	out := b.String()
	for _, want := range []string{"replacement-running", "waiting-for-updated-ready", "alloc-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q does not contain %q", out, want)
		}
	}
}
