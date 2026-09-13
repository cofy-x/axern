package controlplane

import (
	"context"
	"os"
	"strings"
	"time"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	nodecontrol "github.com/cofy-x/axern/runtime/axnoded/internal/controlplane"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	sandboxobs "github.com/cofy-x/axern/runtime/axnoded/internal/observability"
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
	ReportAllocationLifecycle(report nodecontrol.AllocationLifecycleReport) error
	ReportAllocationCapabilityConditions(report nodecontrol.AllocationCapabilityConditionReport)
	AllocationLifecycleHealth() nodecontrol.AllocationLifecycleReporterHealth
	UnacknowledgedAllocationLifecycleIDs() []string
	ReplayDurableAllocationLifecycles() error
}

func (c *Coordinator) ReportCapabilityConditions(allocationID string, conditionSet *capabilityv1.CapabilityConditionSet) {
	if c == nil || c.reporter == nil || c.hasAllocation == nil || !c.hasAllocation(strings.TrimSpace(allocationID)) {
		return
	}
	c.reporter.ReportAllocationCapabilityConditions(nodecontrol.AllocationCapabilityConditionReport{AllocationID: allocationID, ConditionSet: conditionSet})
}

type Options struct {
	GetContainer  func(string) (*container.Container, error)
	HasAllocation func(string) bool
	Reporter      NodeReporter
	Now           func() time.Time
}

type Coordinator struct {
	getContainer  func(string) (*container.Container, error)
	hasAllocation func(string) bool
	reporter      NodeReporter
	now           func() time.Time
}

func (c *Coordinator) AllocationLifecycleHealth() nodecontrol.AllocationLifecycleReporterHealth {
	if c == nil || c.reporter == nil {
		return nodecontrol.AllocationLifecycleReporterHealth{Status: "disabled"}
	}
	return c.reporter.AllocationLifecycleHealth()
}

func (c *Coordinator) UnacknowledgedAllocationLifecycleIDs() []string {
	if c == nil || c.reporter == nil {
		return nil
	}
	return c.reporter.UnacknowledgedAllocationLifecycleIDs()
}

func NewCoordinator(options Options) *Coordinator {
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Coordinator{
		getContainer:  options.GetContainer,
		hasAllocation: options.HasAllocation,
		reporter:      options.Reporter,
		now:           now,
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

func (c *Coordinator) ReportAllocationLifecycle(allocationID string, state commonv1.AllocationLifecycleState, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time) {
	if c == nil || c.reporter == nil || c.hasAllocation == nil {
		return
	}
	allocationID = strings.TrimSpace(allocationID)
	if !c.hasAllocation(allocationID) {
		return
	}
	_, span := sdkobs.Start(context.Background(), sandboxobs.SpanStatusReport,
		attribute.String(sdkobs.AttrAllocationID, allocationID),
		attribute.String(sdkobs.AttrStatus, state.String()),
	)
	defer span.End()
	if err := c.reporter.ReportAllocationLifecycle(nodecontrol.AllocationLifecycleReport{
		AllocationID:     allocationID,
		State:            state,
		ExitCode:         exitCode,
		ExitCodeKnown:    exitCodeKnown,
		Ready:            ready,
		ReadinessMessage: strings.TrimSpace(readinessMessage),
		Message:          message,
		DiagnosticCode:   commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
		ObservedAt:       observedAt,
	}); err != nil {
		logrus.WithError(err).WithField("allocation_id", allocationID).Warn("queue allocation lifecycle report")
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
	return c.reporter.ReportAllocationLifecycle(report)
}

// ContainerExitReport converts a durable runtime exit into the exact typed
// control-plane observation used both by the live reporter and startup outbox
// recovery. A false result identifies a non-allocation/internal container.
func (c *Coordinator) ContainerExitReport(event container.Event) (nodecontrol.AllocationLifecycleReport, bool) {
	if c == nil || c.getContainer == nil || c.hasAllocation == nil || strings.TrimSpace(event.ContainerID) == "" || !c.hasAllocation(event.ContainerID) {
		return nodecontrol.AllocationLifecycleReport{}, false
	}
	ct, err := c.getContainer(event.ContainerID)
	if err != nil || ct == nil || ct.Metadata == nil {
		return nodecontrol.AllocationLifecycleReport{}, false
	}
	report := ContainerExitReportFromContainer(ct, event, c.now())
	return report, report.AllocationID != ""
}

// ContainerExitReportFromContainer is the initialization-order-independent
// shaping contract shared by live runtime observation and startup recovery.
// The caller must already own the durable container record.
func ContainerExitReportFromContainer(ct *container.Container, event container.Event, fallbackObservedAt time.Time) nodecontrol.AllocationLifecycleReport {
	if ct == nil || ct.Metadata == nil {
		return nodecontrol.AllocationLifecycleReport{}
	}
	allocationID := strings.TrimSpace(event.ContainerID)
	if allocationID == "" || strings.TrimSpace(ct.ID) != allocationID {
		return nodecontrol.AllocationLifecycleReport{}
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
		"exit_code":     event.ExitCode,
		"known":         event.ExitCodeKnown,
	}).Debug("reporting exited allocation lifecycle to control plane")
	return nodecontrol.AllocationLifecycleReport{
		AllocationID:   allocationID,
		State:          commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED,
		ExitCode:       event.ExitCode,
		ExitCodeKnown:  event.ExitCodeKnown,
		Message:        message,
		DiagnosticCode: diagnosticCode,
		ObservedAt:     observedAt,
	}
}

func (c *Coordinator) ReplayDurableAllocationLifecycles() error {
	if c == nil || c.reporter == nil {
		return nil
	}
	return c.reporter.ReplayDurableAllocationLifecycles()
}

func NewNodeReporter(
	cfg config.Config,
	runtimeNames func() []string,
	inventory func() (nodeinventory.NodeInventorySnapshot, bool),
	lifecycleOutbox *nodecontrol.AllocationLifecycleOutbox,
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
		lifecycleOutbox,
	), nil
}
