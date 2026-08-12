package controlplane

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	nodecontrol "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

// NodeReporter is the complete reporting dependency used by Coordinator.
// Lifecycle health and the acknowledgement barrier are correctness inputs, so
// they must not be discovered through optional type assertions.
type NodeReporter interface {
	Start()
	Stop()
	NotifyInventoryChanged()
	ReportAllocationStatus(report nodecontrol.AllocationStatusReport) error
	ReportAllocationCapabilityConditions(report nodecontrol.AllocationCapabilityConditionReport)
	AllocationStatusHealth() nodecontrol.AllocationStatusReporterHealth
	UnacknowledgedAllocationStatusIDs() []string
	ReplayDurableAllocationStatuses() error
}

func (c *Coordinator) ReportCapabilityConditions(allocationID string, attempt int64, conditionSet *capabilityv1.CapabilityConditionSet) {
	if c == nil || c.reporter == nil {
		return
	}
	c.reporter.ReportAllocationCapabilityConditions(nodecontrol.AllocationCapabilityConditionReport{AllocationID: allocationID, Attempt: attempt, ConditionSet: conditionSet})
}

type Options struct {
	GetContainer func(string) (*container.Container, error)
	Reporter     NodeReporter
	Now          func() time.Time
}

type Coordinator struct {
	getContainer func(string) (*container.Container, error)
	reporter     NodeReporter
	now          func() time.Time
}

func (c *Coordinator) AllocationStatusHealth() nodecontrol.AllocationStatusReporterHealth {
	if c == nil || c.reporter == nil {
		return nodecontrol.AllocationStatusReporterHealth{Status: "disabled"}
	}
	return c.reporter.AllocationStatusHealth()
}

func (c *Coordinator) UnacknowledgedAllocationStatusIDs() []string {
	if c == nil || c.reporter == nil {
		return nil
	}
	return c.reporter.UnacknowledgedAllocationStatusIDs()
}

func NewCoordinator(options Options) *Coordinator {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{
		getContainer: options.GetContainer,
		reporter:     options.Reporter,
		now:          now,
	}
}

// SetReporter is the production assembly boundary. Taking the concrete
// reporter prevents a disabled *Reporter(nil) from becoming a non-nil Go
// interface; tests inject alternate implementations through NewCoordinator.
func (c *Coordinator) SetReporter(reporter *nodecontrol.Reporter) {
	if c == nil {
		return
	}
	if reporter == nil {
		c.reporter = nil
		return
	}
	c.reporter = reporter
}

func (c *Coordinator) Start() {
	if c == nil {
		return
	}
	if c.reporter != nil {
		c.reporter.Start()
	}
}

func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	if c.reporter != nil {
		c.reporter.Stop()
	}
}

func (c *Coordinator) NotifyInventoryChanged() {
	if c == nil || c.reporter == nil {
		return
	}
	c.reporter.NotifyInventoryChanged()
}

func (c *Coordinator) ReportAllocationStatus(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time) {
	if c == nil || c.reporter == nil {
		return
	}
	allocationID = strings.TrimSpace(allocationID)
	_, span := sdkobs.Start(context.Background(), sandboxobs.SpanStatusReport,
		attribute.String(sdkobs.AttrAllocationID, allocationID),
		attribute.String(sdkobs.AttrStatus, status.String()),
	)
	defer span.End()
	if err := c.reporter.ReportAllocationStatus(nodecontrol.AllocationStatusReport{
		AllocationID:     allocationID,
		Attempt:          attempt,
		Status:           status,
		ExitCode:         exitCode,
		ExitCodeKnown:    exitCodeKnown,
		Ready:            ready,
		ReadinessMessage: strings.TrimSpace(readinessMessage),
		Message:          message,
		DiagnosticCode:   commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		ObservedAt:       observedAt,
	}); err != nil {
		logrus.WithError(err).WithField("allocation_id", allocationID).Warn("queue allocation status report")
	}
}

func (c *Coordinator) ReportContainerExit(event container.Event) error {
	if c == nil || c.reporter == nil || c.getContainer == nil || strings.TrimSpace(event.ContainerID) == "" {
		return nil
	}
	report, ok := c.ContainerExitReport(event)
	if !ok {
		return nil
	}
	return c.reporter.ReportAllocationStatus(report)
}

// ContainerExitReport converts a durable runtime exit into the exact typed
// control-plane observation used both by the live reporter and startup outbox
// recovery. A false result identifies a non-allocation/internal container.
func (c *Coordinator) ContainerExitReport(event container.Event) (nodecontrol.AllocationStatusReport, bool) {
	if c == nil || c.getContainer == nil || strings.TrimSpace(event.ContainerID) == "" {
		return nodecontrol.AllocationStatusReport{}, false
	}
	ct, err := c.getContainer(event.ContainerID)
	if err != nil || ct == nil || ct.Metadata == nil {
		return nodecontrol.AllocationStatusReport{}, false
	}
	report := ContainerExitReportFromContainer(ct, event, c.now())
	return report, report.AllocationID != "" && report.Attempt > 0
}

// ContainerExitReportFromContainer is the initialization-order-independent
// shaping contract shared by live runtime observation and startup recovery.
// The caller must already own the durable container record.
func ContainerExitReportFromContainer(ct *container.Container, event container.Event, fallbackObservedAt time.Time) nodecontrol.AllocationStatusReport {
	if ct == nil || ct.Metadata == nil {
		return nodecontrol.AllocationStatusReport{}
	}
	allocationID := strings.TrimSpace(ct.Metadata.ID)
	if allocationID == "" {
		return nodecontrol.AllocationStatusReport{}
	}
	attempt := AllocationAttemptFromLabels(ct.Metadata.Labels)
	if attempt <= 0 {
		return nodecontrol.AllocationStatusReport{}
	}
	message := strings.TrimSpace(event.Reason)
	diagnosticCode := event.DiagnosticCode
	if message == "" && ct.Status != nil {
		message = strings.TrimSpace(ct.Status.Get().Message)
	}
	if diagnosticCode == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED && ct.Status != nil {
		diagnosticCode = ct.Status.Get().DiagnosticCode
	}
	observedAt := event.ExitedAt.UTC()
	if observedAt.IsZero() {
		observedAt = fallbackObservedAt.UTC()
	}
	logrus.WithFields(logrus.Fields{
		"allocation_id": allocationID,
		"attempt":       attempt,
		"exit_code":     event.ExitCode,
		"known":         event.ExitCodeKnown,
	}).Debug("reporting exited allocation status to control plane")
	return nodecontrol.AllocationStatusReport{
		AllocationID:   allocationID,
		Attempt:        attempt,
		Status:         commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		ExitCode:       event.ExitCode,
		ExitCodeKnown:  event.ExitCodeKnown,
		Message:        message,
		DiagnosticCode: diagnosticCode,
		ObservedAt:     observedAt,
	}
}

func (c *Coordinator) ReplayDurableAllocationStatuses() error {
	if c == nil || c.reporter == nil {
		return nil
	}
	return c.reporter.ReplayDurableAllocationStatuses()
}

func AllocationAttemptFromLabels(labels map[string]string) int64 {
	if len(labels) == 0 {
		return 0
	}
	raw := strings.TrimSpace(labels[workloadidentity.LabelKeyAllocationAttempt])
	if raw == "" {
		return 0
	}
	attempt, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || attempt <= 0 {
		return 0
	}
	return attempt
}

func NewNodeReporter(
	cfg config.Config,
	runtimeNames func() []string,
	inventory func() (nodeinventory.NodeInventorySnapshot, bool),
	statusOutbox *nodecontrol.AllocationStatusOutbox,
) (*nodecontrol.Reporter, error) {
	target := cfg.PluginConfig.ControlPlaneTargetValue()
	if target == "" {
		return nil, nil
	}
	heartbeatInterval, err := cfg.PluginConfig.ControlPlaneHeartbeatIntervalDuration()
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	nodeID := cfg.PluginConfig.ControlPlaneNodeIDValue(hostname)
	return nodecontrol.NewReporter(
		target,
		nodeID,
		cfg.PluginConfig.ControlPlaneNodeTargetValue(),
		cfg.PluginConfig.ControlPlaneNodeAuthTokenValue(),
		cfg.PluginConfig.ControlPlaneTLSCACertValue(),
		cfg.PluginConfig.ControlPlaneTLSCertValue(),
		cfg.PluginConfig.ControlPlaneTLSKeyValue(),
		heartbeatInterval,
		runtimeNames,
		inventory,
		nodecontrol.BuildNodeSummary,
		statusOutbox,
	), nil
}
