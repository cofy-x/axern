package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/lib/go/executionlease"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	nodecontrol "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodecontrolpb "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

type ControlPlaneReporterHealth struct {
	Enabled             bool                              `json:"enabled"`
	AllocationLifecycle AllocationLifecycleReporterHealth `json:"allocationLifecycle"`
}

type AllocationLifecycleReporterHealth struct {
	Status              string     `json:"status"`
	Pending             int        `json:"pending"`
	OldestPendingAt     *time.Time `json:"oldestPendingAt,omitempty"`
	OldestPendingAgeSec float64    `json:"oldestPendingAgeSeconds"`
	InFlight            bool       `json:"inFlight"`
	LastAttemptAt       *time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt       *time.Time `json:"lastSuccessAt,omitempty"`
	LastErrorAt         *time.Time `json:"lastErrorAt,omitempty"`
	LastError           string     `json:"lastError,omitempty"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	NextRetryAt         *time.Time `json:"nextRetryAt,omitempty"`
	RetryDelaySec       float64    `json:"retryDelaySeconds"`
	Stopped             bool       `json:"stopped"`
}

func (h *sandboxService) configureControlPlaneReports() {
	h.controlPlaneReports = servicecontrolplane.NewCoordinator(servicecontrolplane.Options{
		GetContainer: func(id string) (*container.Container, error) {
			if h.containerManager == nil {
				return nil, fmt.Errorf("container manager unavailable")
			}
			return h.containerManager.Get(id)
		},
		HasAllocation: h.allocationController().HasAdmittedAllocation,
	})
}

func (h *sandboxService) initControlPlaneReporter() error {
	reporter, err := servicecontrolplane.NewNodeReporter(h.config, h.NodeInventory, h.allocationLifecycleOutbox)
	if err != nil {
		return err
	}
	if reporter != nil {
		h.controlPlaneReports.SetReporter(reporter)
		reporter.SetInventoryRefresh(h.refreshNodeInventory)
		reporter.SetExecutionLeaseConsumer(h.applyExecutionLeases)
	}
	return nil
}

func (h *sandboxService) applyExecutionLeases(leases []*nodecontrolpb.AllocationExecutionLease, receivedAt time.Time) error {
	ttls := make(map[string]time.Duration, len(leases))
	for _, lease := range leases {
		allocationID := strings.TrimSpace(lease.GetAllocationID())
		ttl := time.Duration(lease.GetTtlSeconds()) * time.Second
		if allocationID == "" || ttl <= 0 || ttl > executionlease.TTL {
			return fmt.Errorf("invalid allocation execution lease")
		}
		if _, duplicate := ttls[allocationID]; duplicate {
			return fmt.Errorf("duplicate allocation execution lease %q", allocationID)
		}
		ttls[allocationID] = ttl
	}
	return h.allocationController().ReplaceExecutionLeases(ttls, receivedAt)
}

func (h *sandboxService) notifyNodeInventoryChanged() {
	if h == nil || h.controlPlaneReports == nil {
		return
	}
	h.controlPlaneReports.NotifyInventoryChanged()
}

func (h *sandboxService) handleContainerExitControlPlaneReport(event container.Event) error {
	if h == nil || h.controlPlaneReports == nil || event.ContainerID == "" {
		return nil
	}
	return h.controlPlaneReports.ReportContainerExit(event)
}

// seedTerminalAllocationLifecycleOutbox closes the crash window between the
// container status checkpoint and outbox persistence. It runs before startup
// reconciliation is allowed to delete terminal runtime/container artifacts.
func (h *sandboxService) seedTerminalAllocationLifecycleOutbox(admittedAllocations map[string]struct{}) error {
	if h == nil || h.containerManager == nil || h.allocationLifecycleOutbox == nil {
		return nil
	}
	for _, item := range h.containerManager.List() {
		if item == nil || item.Metadata == nil || item.Status == nil {
			continue
		}
		status := item.Status.Get()
		if status.State() != apipb.ContainerState_CONTAINER_EXITED {
			continue
		}
		if _, admitted := admittedAllocations[item.ID]; !admitted {
			continue
		}
		exitedAt := container.ParseTimestampTime(status.FinishedAt)
		if exitedAt.IsZero() {
			return fmt.Errorf("recovered terminal allocation %s has an invalid finished timestamp", item.ID)
		}
		report := servicecontrolplane.ContainerExitReportFromContainer(item, container.Event{
			Type:           container.EventTypeExit,
			ContainerID:    item.ID,
			ExitCode:       status.ExitCode,
			ExitedAt:       exitedAt,
			Reason:         status.Message,
			DiagnosticCode: status.DiagnosticCode,
		}, time.Time{})
		if report.AllocationID == "" {
			continue
		}
		observation, err := nodecontrol.AllocationLifecycleObservationFromReport(report)
		if err != nil {
			return fmt.Errorf("shape recovered terminal allocation lifecycle %s: %w", item.ID, err)
		}
		if _, err := h.allocationLifecycleOutbox.Persist(observation); err != nil {
			return err
		}
	}
	return nil
}

func (h *sandboxService) classifyContainerExit(event container.Event) (commonv1.WorkloadDiagnosticCode, string) {
	if event.DiagnosticCode != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return event.DiagnosticCode, event.Reason
	}
	if h != nil && h.allocations != nil {
		if code, message := h.allocations.TerminationIntent(event.ContainerID); code != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
			return code, message
		}
	}
	if !h.allocationExitWasMemoryOOM(event.ContainerID) {
		return event.DiagnosticCode, event.Reason
	}
	metrics.RecordSandboxMemoryOOM("runsc")
	return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_MEMORY_LIMIT_EXCEEDED, "sandbox memory limit exceeded"
}

func (h *sandboxService) allocationExitWasMemoryOOM(allocationID string) bool {
	if h == nil || h.allocations == nil {
		return false
	}
	manifest := h.allocations.EnforcementManifest(allocationID)
	if manifest == nil || manifest.GetMemoryLimitBytes() <= 0 || manifest.GetCgroupPath() == "" {
		return false
	}
	observation, err := hostlinux.ReadCgroupMemoryObservation(manifest.GetCgroupPath())
	if err != nil {
		return false
	}
	return memoryObservationIndicatesOOM(manifest, observation)
}

func memoryObservationIndicatesOOM(manifest *apipb.AllocationEnforcementManifest, observation *hostlinux.CgroupMemoryObservation) bool {
	if manifest == nil || observation == nil || manifest.GetMemoryLimitBytes() <= 0 {
		return false
	}
	return observation.Events["oom_kill"] > manifest.GetInitialMemoryEventOomKill() ||
		observation.Events["oom_group_kill"] > manifest.GetInitialMemoryEventOomGroupKill()
}

func (h *sandboxService) ReportAllocationLifecycle(allocationID string, status commonv1.AllocationLifecycleState, exitCode *int32, ready bool, readinessMessage string, message string, observedAt time.Time) {
	if h == nil || h.controlPlaneReports == nil {
		return
	}
	h.controlPlaneReports.ReportAllocationLifecycle(allocationID, status, exitCode, ready, readinessMessage, message, observedAt)
}

func (h *sandboxService) ControlPlaneReporterHealth() ControlPlaneReporterHealth {
	if h == nil || h.controlPlaneReports == nil {
		return ControlPlaneReporterHealth{
			AllocationLifecycle: AllocationLifecycleReporterHealth{Status: "disabled"},
		}
	}
	health := h.controlPlaneReports.AllocationLifecycleHealth()
	return ControlPlaneReporterHealth{
		Enabled: health.Status != "disabled",
		AllocationLifecycle: AllocationLifecycleReporterHealth{
			Status:              health.Status,
			Pending:             health.Pending,
			OldestPendingAt:     health.OldestPendingAt,
			OldestPendingAgeSec: health.OldestPendingAgeSec,
			InFlight:            health.InFlight,
			LastAttemptAt:       health.LastAttemptAt,
			LastSuccessAt:       health.LastSuccessAt,
			LastErrorAt:         health.LastErrorAt,
			LastError:           health.LastError,
			ConsecutiveFailures: health.ConsecutiveFailures,
			NextRetryAt:         health.NextRetryAt,
			RetryDelaySec:       health.RetryDelaySec,
			Stopped:             health.Stopped,
		},
	}
}
