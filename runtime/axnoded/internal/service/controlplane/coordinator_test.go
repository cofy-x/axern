package controlplane

import (
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	nodecontrol "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type statusReporterSpy struct {
	report            nodecontrol.AllocationLifecycleReport
	health            nodecontrol.AllocationLifecycleReporterHealth
	unacknowledgedIDs []string
}

func (s *statusReporterSpy) ReportAllocationLifecycle(report nodecontrol.AllocationLifecycleReport) error {
	s.report = report
	return nil
}

func (s *statusReporterSpy) Start() {}

func (s *statusReporterSpy) Stop() {}

func (s *statusReporterSpy) NotifyInventoryChanged() {}

func (s *statusReporterSpy) ReportAllocationCapabilityConditions(nodecontrol.AllocationCapabilityConditionReport) {
}

func (s *statusReporterSpy) AllocationLifecycleHealth() nodecontrol.AllocationLifecycleReporterHealth {
	return s.health
}

func (s *statusReporterSpy) UnacknowledgedAllocationLifecycleIDs() []string {
	return append([]string(nil), s.unacknowledgedIDs...)
}

func (s *statusReporterSpy) ReplayDurableAllocationLifecycles() error { return nil }

func TestAllocationLifecycleHealthUsesReporterSnapshot(t *testing.T) {
	reporter := &statusReporterSpy{health: nodecontrol.AllocationLifecycleReporterHealth{
		Status:              "retrying",
		Pending:             4,
		ConsecutiveFailures: 3,
	}}
	health := NewCoordinator(Options{Reporter: reporter}).AllocationLifecycleHealth()
	if health.Status != "retrying" || health.Pending != 4 || health.ConsecutiveFailures != 3 {
		t.Fatalf("allocation lifecycle health = %#v", health)
	}

	disabled := NewCoordinator(Options{}).AllocationLifecycleHealth()
	if disabled.Status != "disabled" {
		t.Fatalf("disabled health = %#v", disabled)
	}
}

func TestSetReporterKeepsConcreteNilDisabled(t *testing.T) {
	coordinator := NewCoordinator(Options{})
	coordinator.SetReporter(nil)
	if got := coordinator.AllocationLifecycleHealth().Status; got != "disabled" {
		t.Fatalf("allocation lifecycle health = %q, want disabled", got)
	}
	if err := coordinator.ReportContainerExit(container.Event{ContainerID: "alloc-1"}); err != nil {
		t.Fatalf("ReportContainerExit() with disabled reporter error = %v", err)
	}
}

func TestUnacknowledgedAllocationLifecycleIDsUsesReporterSnapshot(t *testing.T) {
	reporter := &statusReporterSpy{unacknowledgedIDs: []string{"alloc-b", "alloc-a"}}
	got := NewCoordinator(Options{Reporter: reporter}).UnacknowledgedAllocationLifecycleIDs()
	if len(got) != 2 || got[0] != "alloc-b" || got[1] != "alloc-a" {
		t.Fatalf("unacknowledged allocation ids = %#v", got)
	}
	if got := NewCoordinator(Options{}).UnacknowledgedAllocationLifecycleIDs(); len(got) != 0 {
		t.Fatalf("disabled unacknowledged allocation ids = %#v, want empty", got)
	}
}

func TestReportAllocationLifecycleShapesReport(t *testing.T) {
	reporter := &statusReporterSpy{}
	coordinator := NewCoordinator(Options{
		Reporter:      reporter,
		HasAllocation: func(id string) bool { return id == "alloc-123" },
	})
	observedAt := time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)

	coordinator.ReportAllocationLifecycle(" alloc-123 ", commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, 0, false, true, " ready ", "ok", observedAt)

	if reporter.report.AllocationID != "alloc-123" {
		t.Fatalf("allocation id = %q, want trimmed alloc-123", reporter.report.AllocationID)
	}
	if reporter.report.State != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE {
		t.Fatalf("report status = %v, want RUNNING", reporter.report.State)
	}
	if !reporter.report.Ready || reporter.report.ReadinessMessage != "ready" {
		t.Fatalf("ready/message = %v/%q, want true/ready", reporter.report.Ready, reporter.report.ReadinessMessage)
	}
	if !reporter.report.ObservedAt.Equal(observedAt) {
		t.Fatalf("observedAt = %v, want %v", reporter.report.ObservedAt, observedAt)
	}
}

func TestReportContainerExitUsesAllocationIdentity(t *testing.T) {
	reporter := &statusReporterSpy{}
	managerContainer := &container.Container{
		ID:       "alloc-123",
		Metadata: &apipb.ContainerMetadata{},
	}
	coordinator := NewCoordinator(Options{
		Reporter:      reporter,
		HasAllocation: func(id string) bool { return id == "alloc-123" },
		GetContainer: func(string) (*container.Container, error) {
			return managerContainer, nil
		},
	})

	coordinator.ReportContainerExit(container.Event{
		ContainerID:    "alloc-123",
		ExitCode:       42,
		ExitCodeKnown:  true,
		DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED,
		ExitedAt:       time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC),
	})

	if reporter.report.AllocationID != "alloc-123" {
		t.Fatalf("allocation id = %q, want alloc-123", reporter.report.AllocationID)
	}
	if reporter.report.State != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
		t.Fatalf("state = %v, want STOPPED", reporter.report.State)
	}
	if reporter.report.ExitCode != 42 || !reporter.report.ExitCodeKnown {
		t.Fatalf("exit = %d/%v, want 42/true", reporter.report.ExitCode, reporter.report.ExitCodeKnown)
	}
	if reporter.report.DiagnosticCode != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED {
		t.Fatalf("diagnostic code = %v, want MEMORY_LIMIT_EXCEEDED", reporter.report.DiagnosticCode)
	}
}

func TestReportContainerExitSkipsContainerWithoutExplicitID(t *testing.T) {
	reporter := &statusReporterSpy{}
	coordinator := NewCoordinator(Options{
		Reporter:      reporter,
		HasAllocation: func(string) bool { return true },
		GetContainer: func(string) (*container.Container, error) {
			return &container.Container{
				Metadata: &apipb.ContainerMetadata{},
			}, nil
		},
	})

	coordinator.ReportContainerExit(container.Event{ContainerID: "alloc-123"})

	if reporter.report.AllocationID != "" {
		t.Fatalf("reported allocation id = %q, want no report", reporter.report.AllocationID)
	}
}
