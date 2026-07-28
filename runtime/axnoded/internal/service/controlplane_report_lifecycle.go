package service

import (
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	servicecontrolplane "github.com/cofy-x/axern/runtime/axnoded/internal/service/controlplane"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type ControlPlaneReporterHealth struct {
	Enabled          bool                           `json:"enabled"`
	AllocationStatus AllocationStatusReporterHealth `json:"allocationStatus"`
}

type AllocationStatusReporterHealth struct {
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
	})
}

func (h *sandboxService) initControlPlaneReporter() error {
	reporter, err := servicecontrolplane.NewNodeReporter(h.config, h.runtimeHandlers.Names, h.NodeInventory)
	if err != nil {
		return err
	}
	h.controlPlaneReports.SetReporter(reporter)
	if reporter != nil {
		reporter.SetInventoryRefresh(h.refreshNodeInventory)
	}
	return nil
}

func (h *sandboxService) notifyNodeInventoryChanged() {
	if h == nil || h.controlPlaneReports == nil {
		return
	}
	h.controlPlaneReports.NotifyInventoryChanged()
}

func (h *sandboxService) handleContainerExitControlPlaneReport(event container.Event) {
	if h == nil || h.controlPlaneReports == nil || event.ContainerID == "" {
		return
	}
	h.controlPlaneReports.ReportContainerExit(event)
}

func (h *sandboxService) ReportAllocationStatus(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time) {
	if h == nil || h.controlPlaneReports == nil {
		return
	}
	h.controlPlaneReports.ReportAllocationStatus(allocationID, attempt, status, exitCode, exitCodeKnown, ready, readinessMessage, message, observedAt)
}

func (h *sandboxService) ControlPlaneReporterHealth() ControlPlaneReporterHealth {
	if h == nil || h.controlPlaneReports == nil {
		return ControlPlaneReporterHealth{
			AllocationStatus: AllocationStatusReporterHealth{Status: "disabled"},
		}
	}
	health := h.controlPlaneReports.AllocationStatusHealth()
	return ControlPlaneReporterHealth{
		Enabled: health.Status != "disabled",
		AllocationStatus: AllocationStatusReporterHealth{
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
