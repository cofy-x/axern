package service

import (
	"testing"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	controlplane "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocation"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	cmap "github.com/orcaman/concurrent-map/v2"
)

type fakeAllocationLifecycleReporter struct {
	lastID           string
	status           commonv1.AllocationLifecycleState
	exitCode         int32
	known            bool
	ready            bool
	readinessMessage string
	message          string
	diagnosticCode   commonv1.WorkloadDiagnosticCode
}

func (f *fakeAllocationLifecycleReporter) ReportAllocationLifecycle(report controlplane.AllocationLifecycleReport) error {
	f.lastID = report.AllocationID
	f.status = report.State
	f.exitCode = report.ExitCode
	f.known = report.ExitCodeKnown
	f.ready = report.Ready
	f.readinessMessage = report.ReadinessMessage
	f.message = report.Message
	f.diagnosticCode = report.DiagnosticCode
	return nil
}

func (f *fakeAllocationLifecycleReporter) Start() {}

func (f *fakeAllocationLifecycleReporter) Stop() {}

func (f *fakeAllocationLifecycleReporter) NotifyInventoryChanged() {}

func (f *fakeAllocationLifecycleReporter) ReportAllocationCapabilityConditions(controlplane.AllocationCapabilityConditionReport) {
}

func (f *fakeAllocationLifecycleReporter) AllocationLifecycleHealth() controlplane.AllocationLifecycleReporterHealth {
	return controlplane.AllocationLifecycleReporterHealth{Status: "idle"}
}

func (f *fakeAllocationLifecycleReporter) UnacknowledgedAllocationLifecycleIDs() []string {
	return nil
}

func (f *fakeAllocationLifecycleReporter) ReplayDurableAllocationLifecycles() error { return nil }

func TestConfigureAllocationControllerKeepsSingleAdmissionAuthority(t *testing.T) {
	service := &sandboxService{store: storetest.NewMockStore()}
	service.configureAllocationController()
	first := service.allocations
	if first == nil {
		t.Fatal("configureAllocationController() did not install an allocation authority")
	}

	service.configureAllocationController()
	if service.allocations != first {
		t.Fatal("configureAllocationController() replaced the allocation authority captured by reporting")
	}
}

func TestContainerExitObserverReportsAllocationLifecycleState(t *testing.T) {
	tmpDir := t.TempDir()
	manager, err := container.NewManager(tmpDir, cmap.New[contract.RuntimeHandler](), make(chan bool, 1))
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.StoreMetadata("alloc-123", &apipb.ContainerMetadata{})

	reporter := &fakeAllocationLifecycleReporter{}
	service := &sandboxService{
		containerManager: manager,
		controlPlaneReports: servicecontrolplane.NewCoordinator(servicecontrolplane.Options{
			Reporter:      reporter,
			HasAllocation: func(id string) bool { return id == "alloc-123" },
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
	if reporter.status != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED {
		t.Fatalf("reported state = %v, want STOPPED", reporter.status)
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
	if err := manager.StoreMetadata("alloc-recovered", &apipb.ContainerMetadata{}); err != nil {
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
	outbox := controlplane.NewAllocationLifecycleOutbox(stateStore)
	allocationController := allocation.NewController(allocation.Options{Store: stateStore})
	if _, err := allocationController.ReplaceCapabilityAdmission("alloc-recovered", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil, nil, time.Now().UTC()); err != nil {
		t.Fatalf("ReplaceCapabilityAdmission() error = %v", err)
	}
	service := &sandboxService{
		containerManager:          manager,
		allocationLifecycleOutbox: outbox,
		allocations:               allocationController,
	}

	if err := service.seedTerminalAllocationLifecycleOutbox(map[string]struct{}{"alloc-recovered": {}}); err != nil {
		t.Fatalf("seedTerminalAllocationLifecycleOutbox() error = %v", err)
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

	if replayed.GetAllocationID() != "alloc-recovered" {
		t.Fatalf("replayed allocation = %q, want alloc-recovered", replayed.GetAllocationID())
	}
	if replayed.GetState() != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED || replayed.GetExitCode() != 42 || !replayed.GetExitCodeKnown() {
		t.Fatalf("replayed lifecycle = status %v exit %d known %v", replayed.GetState(), replayed.GetExitCode(), replayed.GetExitCodeKnown())
	}
	if replayed.GetMessage() != "sandbox exited" || replayed.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED {
		t.Fatalf("replayed diagnostics = message %q code %v", replayed.GetMessage(), replayed.GetDiagnosticCode())
	}
}
