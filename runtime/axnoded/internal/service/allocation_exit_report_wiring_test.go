package service

import (
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	controlplane "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type fakeAllocationStatusReporter struct {
	lastID           string
	attempt          int64
	status           commonv1.AllocationStatus
	exitCode         int32
	known            bool
	ready            bool
	readinessMessage string
	message          string
	diagnosticCode   commonv1.WorkloadDiagnosticCode
}

func (f *fakeAllocationStatusReporter) ReportAllocationStatus(report controlplane.AllocationStatusReport) error {
	f.lastID = report.AllocationID
	f.attempt = report.Attempt
	f.status = report.Status
	f.exitCode = report.ExitCode
	f.known = report.ExitCodeKnown
	f.ready = report.Ready
	f.readinessMessage = report.ReadinessMessage
	f.message = report.Message
	f.diagnosticCode = report.DiagnosticCode
	return nil
}

func (f *fakeAllocationStatusReporter) Start() {}

func (f *fakeAllocationStatusReporter) Stop() {}

func (f *fakeAllocationStatusReporter) NotifyInventoryChanged() {}

func (f *fakeAllocationStatusReporter) ReportAllocationCapabilityConditions(controlplane.AllocationCapabilityConditionReport) {
}

func (f *fakeAllocationStatusReporter) AllocationStatusHealth() controlplane.AllocationStatusReporterHealth {
	return controlplane.AllocationStatusReporterHealth{Status: "idle"}
}

func (f *fakeAllocationStatusReporter) UnacknowledgedAllocationStatusIDs() []string {
	return nil
}

func (f *fakeAllocationStatusReporter) ReplayDurableAllocationStatuses() error { return nil }

func TestContainerExitObserverReportsAllocationStatus(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := container.NewManager(tmpDir, cmap.New[contract.RuntimeHandler](), make(chan bool, 1))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.StoreMetadata("alloc-123", &apipb.ContainerMetadata{
		ID: "alloc-123",
		Labels: map[string]string{
			workloadidentity.LabelKeyAllocationAttempt: "7",
		},
	})

	reporter := &fakeAllocationStatusReporter{}
	service := &sandboxService{
		containerManager: manager,
		controlPlaneReports: servicecontrolplane.NewCoordinator(servicecontrolplane.Options{
			Reporter: reporter,
			GetContainer: func(id string) (*container.Container, error) {
				return manager.Get(id)
			},
		}),
	}

	service.handleContainerExitControlPlaneReport(container.Event{
		Type:          container.EventTypeExit,
		ContainerID:   "alloc-123",
		ExitCode:      0,
		ExitCodeKnown: true,
		ExitedAt:      time.Now().UTC(),
	})

	if reporter.lastID != "alloc-123" {
		t.Fatalf("reported allocation id = %q, want alloc-123", reporter.lastID)
	}
	if reporter.attempt != 7 {
		t.Fatalf("reported attempt = %d, want 7", reporter.attempt)
	}
	if reporter.status != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED {
		t.Fatalf("reported status = %v, want EXITED", reporter.status)
	}
	if reporter.exitCode != 0 || !reporter.known {
		t.Fatalf("reported exit = code %d known %v, want 0 true", reporter.exitCode, reporter.known)
	}
}

func TestTerminalCheckpointSeedsDurableOutboxBeforeContainerCleanup(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := container.NewManager(tmpDir, cmap.New[contract.RuntimeHandler](), make(chan bool, 1))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := manager.StoreMetadata("alloc-recovered", &apipb.ContainerMetadata{
		ID: "alloc-recovered",
		Labels: map[string]string{
			workloadidentity.LabelKeyAllocationAttempt: "3",
		},
	}); err != nil {
		t.Fatalf("StoreMetadata() error = %v", err)
	}
	finishedAt := time.Date(2026, 8, 11, 12, 34, 56, 0, time.UTC)
	if err := manager.SetExit(
		"alloc-recovered",
		42,
		true,
		finishedAt,
		"sandbox exited",
		commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED,
	); err != nil {
		t.Fatalf("SetExit() error = %v", err)
	}

	stateStore := storetest.NewMockStore()
	outbox := controlplane.NewAllocationStatusOutbox(stateStore)
	service := &sandboxService{
		containerManager:       manager,
		allocationStatusOutbox: outbox,
	}

	if err := service.seedTerminalAllocationStatusOutbox(); err != nil {
		t.Fatalf("seedTerminalAllocationStatusOutbox() error = %v", err)
	}
	if err := manager.DeleteAfterConfirmedRuntimeAbsence("alloc-recovered"); err != nil {
		t.Fatalf("DeleteAfterConfirmedRuntimeAbsence() error = %v", err)
	}
	observations, err := outbox.Replay()
	if err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("replayed observations = %d, want 1", len(observations))
	}
	replayed := observations[0]

	if replayed.GetAllocationID() != "alloc-recovered" || replayed.GetAttempt() != 3 {
		t.Fatalf("replayed allocation = %q attempt %d, want alloc-recovered attempt 3", replayed.GetAllocationID(), replayed.GetAttempt())
	}
	if replayed.GetStatus() != commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED || replayed.GetExitCode() != 42 || !replayed.GetExitCodeKnown() {
		t.Fatalf("replayed lifecycle = status %v exit %d known %v", replayed.GetStatus(), replayed.GetExitCode(), replayed.GetExitCodeKnown())
	}
	if replayed.GetMessage() != "sandbox exited" || replayed.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED {
		t.Fatalf("replayed diagnostics = message %q code %v", replayed.GetMessage(), replayed.GetDiagnosticCode())
	}
}
