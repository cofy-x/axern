package allocation

import (
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/service/probes"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestReportStartRunningStatusMarksReadyWithoutReadinessProbe(t *testing.T) {
	recorder := &statusReportRecorder{}
	controller := NewController(Options{ReportStatus: recorder.Report})

	controller.reportStartRunningStatus("alloc-123", startplan.ExtraConfig{AllocationAttempt: 3}, time.Now().UTC())

	if recorder.lastID != "alloc-123" {
		t.Fatalf("reported allocation id = %q, want alloc-123", recorder.lastID)
	}
	if recorder.attempt != 3 {
		t.Fatalf("reported attempt = %d, want 3", recorder.attempt)
	}
	if recorder.status != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING || !recorder.ready {
		t.Fatalf("reported status/ready = %v/%v, want RUNNING/true", recorder.status, recorder.ready)
	}
	if recorder.readinessMessage != "" {
		t.Fatalf("readiness message = %q, want empty", recorder.readinessMessage)
	}
}

func TestReportStartRunningStatusDefersToReadinessProbe(t *testing.T) {
	recorder := &statusReportRecorder{}
	controller := NewController(Options{ReportStatus: recorder.Report})

	controller.reportStartRunningStatus("alloc-123", startplan.ExtraConfig{AllocationAttempt: 3, ReadinessProbe: &probes.Config{}}, time.Now().UTC())

	if recorder.lastID != "" {
		t.Fatalf("reported allocation id = %q, want no report", recorder.lastID)
	}
}

type statusReportRecorder struct {
	lastID           string
	attempt          int64
	status           commonv1.AllocationStatus
	ready            bool
	readinessMessage string
}

func (r *statusReportRecorder) Report(allocationID string, attempt int64, status commonv1.AllocationStatus, _ int32, _ bool, ready bool, readinessMessage string, _ string, _ time.Time) {
	r.lastID = allocationID
	r.attempt = attempt
	r.status = status
	r.ready = ready
	r.readinessMessage = readinessMessage
}
