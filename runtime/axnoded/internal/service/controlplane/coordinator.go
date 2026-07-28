package controlplane

import (
	"context"
	"os"
	"sort"
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
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel/attribute"
)

type AllocationStatusReporter interface {
	ReportAllocationStatus(report nodecontrol.AllocationStatusReport)
}

type Options struct {
	GetContainer func(string) (*container.Container, error)
	Reporter     AllocationStatusReporter
	Now          func() time.Time
}

type Coordinator struct {
	getContainer func(string) (*container.Container, error)
	reporter     AllocationStatusReporter
	now          func() time.Time
}

func (c *Coordinator) AllocationStatusHealth() nodecontrol.AllocationStatusReporterHealth {
	if c == nil || c.reporter == nil {
		return nodecontrol.AllocationStatusReporterHealth{Status: "disabled"}
	}
	provider, ok := c.reporter.(interface {
		AllocationStatusHealth() nodecontrol.AllocationStatusReporterHealth
	})
	if !ok {
		return nodecontrol.AllocationStatusReporterHealth{Status: "unavailable"}
	}
	return provider.AllocationStatusHealth()
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

func (c *Coordinator) SetReporter(reporter AllocationStatusReporter) {
	if c == nil {
		return
	}
	c.reporter = reporter
}

func (c *Coordinator) Start() {
	if c == nil {
		return
	}
	reporter, ok := c.reporter.(interface{ Start() })
	if ok && reporter != nil {
		reporter.Start()
	}
}

func (c *Coordinator) Stop() {
	if c == nil {
		return
	}
	reporter, ok := c.reporter.(interface{ Stop() })
	if ok && reporter != nil {
		reporter.Stop()
	}
}

func (c *Coordinator) NotifyInventoryChanged() {
	if c == nil || c.reporter == nil {
		return
	}
	reporter, ok := c.reporter.(interface{ NotifyInventoryChanged() })
	if ok {
		reporter.NotifyInventoryChanged()
	}
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
	c.reporter.ReportAllocationStatus(nodecontrol.AllocationStatusReport{
		AllocationID:     allocationID,
		Attempt:          attempt,
		Status:           status,
		ExitCode:         exitCode,
		ExitCodeKnown:    exitCodeKnown,
		Ready:            ready,
		ReadinessMessage: strings.TrimSpace(readinessMessage),
		Message:          message,
		ObservedAt:       observedAt,
	})
}

func (c *Coordinator) ReportContainerExit(event container.Event) {
	if c == nil || c.reporter == nil || c.getContainer == nil || strings.TrimSpace(event.ContainerID) == "" {
		return
	}
	ct, err := c.getContainer(event.ContainerID)
	if err != nil || ct == nil || ct.Metadata == nil {
		return
	}
	allocationID := strings.TrimSpace(ct.Metadata.ID)
	if allocationID == "" {
		return
	}
	attempt := AllocationAttemptFromLabels(ct.Metadata.Labels)
	if attempt <= 0 {
		return
	}
	message := strings.TrimSpace(event.Reason)
	if message == "" && ct.Status != nil {
		message = strings.TrimSpace(ct.Status.Get().Message)
	}
	observedAt := event.ExitedAt.UTC()
	if observedAt.IsZero() {
		observedAt = c.now()
	}
	logrus.WithFields(logrus.Fields{
		"allocation_id": allocationID,
		"attempt":       attempt,
		"exit_code":     event.ExitCode,
		"known":         event.ExitCodeKnown,
	}).Debug("reporting exited allocation status to control plane")
	c.reporter.ReportAllocationStatus(nodecontrol.AllocationStatusReport{
		AllocationID:  allocationID,
		Attempt:       attempt,
		Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED,
		ExitCode:      event.ExitCode,
		ExitCodeKnown: event.ExitCodeKnown,
		Message:       message,
		ObservedAt:    observedAt,
	})
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

func NewNodeReporter(cfg config.Config, runtimeNames func() []string, inventory func() (nodeinventory.NodeInventorySnapshot, bool)) (*nodecontrol.Reporter, error) {
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
	), nil
}

func DefaultNodeCapabilities(cfg config.Config) []string {
	capabilities := cfg.PluginConfig.ControlPlaneNodeCapabilitiesValue()
	seen := make(map[string]struct{}, len(capabilities)+2)
	out := make([]string, 0, len(capabilities)+2)
	for _, capability := range capabilities {
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}

	for _, implicit := range []string{
		"feature:ports",
		"network:" + cfg.PluginConfig.NetworkConfig.NatBackend,
	} {
		if implicit == "" || implicit == "network:" {
			continue
		}
		if _, ok := seen[implicit]; ok {
			continue
		}
		seen[implicit] = struct{}{}
		out = append(out, implicit)
	}
	sort.Strings(out)
	return out
}
