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

func (f *fakeAllocationStatusReporter) ReportAllocationStatus(report controlplane.AllocationStatusReport) {
	f.lastID = report.AllocationID
	f.attempt = report.Attempt
	f.status = report.Status
	f.exitCode = report.ExitCode
	f.known = report.ExitCodeKnown
	f.ready = report.Ready
	f.readinessMessage = report.ReadinessMessage
	f.message = report.Message
	f.diagnosticCode = report.DiagnosticCode
}

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
