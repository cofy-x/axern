package allocation

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestReportStartRunningStatusMarksReadyWithoutReadinessProbe(t *testing.T) {
	recorder := &statusReportRecorder{}
	controller := NewController(Options{ReportStatus: recorder.Report})

	controller.reportStartRunningStatus("alloc-123", time.Now().UTC())

	if recorder.lastID != "alloc-123" {
		t.Fatalf("reported allocation id = %q, want alloc-123", recorder.lastID)
	}
	if recorder.status != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE || !recorder.ready {
		t.Fatalf("reported status/ready = %v/%v, want RUNNING/true", recorder.status, recorder.ready)
	}
	if recorder.readinessMessage != "" {
		t.Fatalf("readiness message = %q, want empty", recorder.readinessMessage)
	}
}

type statusReportRecorder struct {
	lastID           string
	status           commonv1.AllocationLifecycleState
	ready            bool
	readinessMessage string
}

func (r *statusReportRecorder) Report(allocationID string, status commonv1.AllocationLifecycleState, _ *int32, ready bool, readinessMessage string, _ string, _ time.Time) {
	r.lastID = allocationID
	r.status = status
	r.ready = ready
	r.readinessMessage = readinessMessage
}
