package controlplane

import (
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	nodecontrol "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type statusReporterSpy struct {
	report nodecontrol.AllocationStatusReport
	health nodecontrol.AllocationStatusReporterHealth
}

func (s *statusReporterSpy) ReportAllocationStatus(report nodecontrol.AllocationStatusReport) {
	s.report = report
}

func (s *statusReporterSpy) AllocationStatusHealth() nodecontrol.AllocationStatusReporterHealth {
	return s.health
}

func TestAllocationStatusHealthUsesReporterSnapshot(t *testing.T) {
	reporter := &statusReporterSpy{health: nodecontrol.AllocationStatusReporterHealth{
		Status:              "retrying",
		Pending:             4,
		ConsecutiveFailures: 3,
	}}
	health := NewCoordinator(Options{Reporter: reporter}).AllocationStatusHealth()
	if health.Status != "retrying" || health.Pending != 4 || health.ConsecutiveFailures != 3 {
		t.Fatalf("allocation status health = %#v", health)
	}

	disabled := NewCoordinator(Options{}).AllocationStatusHealth()
	if disabled.Status != "disabled" {
		t.Fatalf("disabled health = %#v", disabled)
	}
}

func TestReportAllocationStatusShapesReport(t *testing.T) {
	reporter := &statusReporterSpy{}
	coordinator := NewCoordinator(Options{Reporter: reporter})
	observedAt := time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)

	coordinator.ReportAllocationStatus(" alloc-123 ", 7, commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, 0, false, true, " ready ", "ok", observedAt)

	if reporter.report.AllocationID != "alloc-123" {
		t.Fatalf("allocation id = %q, want trimmed alloc-123", reporter.report.AllocationID)
	}
	if reporter.report.Attempt != 7 || reporter.report.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING {
		t.Fatalf("report attempt/status = %d/%v, want 7/RUNNING", reporter.report.Attempt, reporter.report.Status)
	}
	if !reporter.report.Ready || reporter.report.ReadinessMessage != "ready" {
		t.Fatalf("ready/message = %v/%q, want true/ready", reporter.report.Ready, reporter.report.ReadinessMessage)
	}
	if !reporter.report.ObservedAt.Equal(observedAt) {
		t.Fatalf("observedAt = %v, want %v", reporter.report.ObservedAt, observedAt)
	}
}

func TestReportContainerExitUsesAllocationAttemptLabel(t *testing.T) {
	reporter := &statusReporterSpy{}
	managerContainer := &container.Container{
		Metadata: &apipb.ContainerMetadata{
			ID: " alloc-123 ",
			Labels: map[string]string{
				workloadidentity.LabelKeyAllocationAttempt: "7",
			},
		},
	}
	coordinator := NewCoordinator(Options{
		Reporter: reporter,
		GetContainer: func(string) (*container.Container, error) {
			return managerContainer, nil
		},
	})

	coordinator.ReportContainerExit(container.Event{
		ContainerID:   "alloc-123",
		ExitCode:      42,
		ExitCodeKnown: true,
		ExitedAt:      time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC),
	})

	if reporter.report.AllocationID != "alloc-123" {
		t.Fatalf("allocation id = %q, want alloc-123", reporter.report.AllocationID)
	}
	if reporter.report.Attempt != 7 {
		t.Fatalf("attempt = %d, want 7", reporter.report.Attempt)
	}
	if reporter.report.Status != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("status = %v, want EXITED", reporter.report.Status)
	}
	if reporter.report.ExitCode != 42 || !reporter.report.ExitCodeKnown {
		t.Fatalf("exit = %d/%v, want 42/true", reporter.report.ExitCode, reporter.report.ExitCodeKnown)
	}
}

func TestReportContainerExitSkipsEmptyMetadataID(t *testing.T) {
	reporter := &statusReporterSpy{}
	coordinator := NewCoordinator(Options{
		Reporter: reporter,
		GetContainer: func(string) (*container.Container, error) {
			return &container.Container{
				Metadata: &apipb.ContainerMetadata{
					ID: " ",
					Labels: map[string]string{
						workloadidentity.LabelKeyAllocationAttempt: "7",
					},
				},
			}, nil
		},
	})

	coordinator.ReportContainerExit(container.Event{ContainerID: "alloc-123"})

	if reporter.report.AllocationID != "" {
		t.Fatalf("reported allocation id = %q, want no report", reporter.report.AllocationID)
	}
}

func TestAllocationAttemptFromLabelsRejectsInvalidValues(t *testing.T) {
	if got := AllocationAttemptFromLabels(nil); got != 0 {
		t.Fatalf("nil labels attempt = %d, want 0", got)
	}
	if got := AllocationAttemptFromLabels(map[string]string{workloadidentity.LabelKeyAllocationAttempt: "abc"}); got != 0 {
		t.Fatalf("invalid attempt = %d, want 0", got)
	}
	if got := AllocationAttemptFromLabels(map[string]string{workloadidentity.LabelKeyAllocationAttempt: "-1"}); got != 0 {
		t.Fatalf("negative attempt = %d, want 0", got)
	}
	if got := AllocationAttemptFromLabels(map[string]string{workloadidentity.LabelKeyAllocationAttempt: " 9 "}); got != 9 {
		t.Fatalf("attempt = %d, want 9", got)
	}
}
