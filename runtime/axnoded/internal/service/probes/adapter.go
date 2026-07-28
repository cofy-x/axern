package probes

import (
	"context"
	"fmt"
	"strings"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/sandboxaccess"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type AdapterOptions struct {
	GetContainer            func(string) (*container.Container, error)
	SandboxAccess           func() *sandboxaccess.Accessor
	SandboxProbe            func(string, *Config) (bool, string)
	ExternalPortProbe       func(ctx context.Context, allocationID string, port int32) error
	Observer                Observer
	StopReadiness           func(string)
	StopLiveness            func(string)
	Report                  func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	CleanupFailedStart      func(context.Context, string)
	LivenessFailureTimeout  time.Duration
	DefaultExecutionTimeout time.Duration
	Now                     func() time.Time
}

type Adapter struct {
	getContainer            func(string) (*container.Container, error)
	sandboxAccess           func() *sandboxaccess.Accessor
	sandboxProbe            func(string, *Config) (bool, string)
	externalPortProbe       func(ctx context.Context, allocationID string, port int32) error
	observer                Observer
	stopReadiness           func(string)
	stopLiveness            func(string)
	report                  func(allocationID string, attempt int64, status commonv1.AllocationStatus, exitCode int32, exitCodeKnown bool, ready bool, readinessMessage string, message string, observedAt time.Time)
	cleanupFailedStart      func(context.Context, string)
	livenessFailureTimeout  time.Duration
	defaultExecutionTimeout time.Duration
	now                     func() time.Time
}

func NewAdapter(options AdapterOptions) *Adapter {
	livenessFailureTimeout := options.LivenessFailureTimeout
	if livenessFailureTimeout <= 0 {
		livenessFailureTimeout = 10 * time.Second
	}
	defaultExecutionTimeout := options.DefaultExecutionTimeout
	if defaultExecutionTimeout <= 0 {
		defaultExecutionTimeout = 2 * time.Second
	}
	now := options.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Adapter{
		getContainer:            options.GetContainer,
		sandboxAccess:           options.SandboxAccess,
		sandboxProbe:            options.SandboxProbe,
		externalPortProbe:       options.ExternalPortProbe,
		observer:                options.Observer,
		stopReadiness:           options.StopReadiness,
		stopLiveness:            options.StopLiveness,
		report:                  options.Report,
		cleanupFailedStart:      options.CleanupFailedStart,
		livenessFailureTimeout:  livenessFailureTimeout,
		defaultExecutionTimeout: defaultExecutionTimeout,
		now:                     now,
	}
}

func (a *Adapter) ExecuteReadinessProbe(containerID string, probe *Config) (bool, string) {
	sandboxStarted := time.Now()
	ok, detail := a.executeReadinessSandboxProbe(containerID, probe)
	a.recordProbeStage(probe, "sandbox", ok, classifyProbeFailure(detail), time.Since(sandboxStarted))
	if !ok || probe == nil || a == nil || a.externalPortProbe == nil {
		return ok, detail
	}
	port := probePort(probe)
	if port <= 0 {
		return ok, detail
	}
	timeout := time.Duration(probe.TimeoutMilliseconds) * time.Millisecond
	if timeout <= 0 {
		timeout = a.defaultExecutionTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	externalStarted := time.Now()
	if err := a.externalPortProbe(ctx, containerID, port); err != nil {
		a.recordProbeStage(probe, "external_port", false, classifyProbeFailure(err.Error()), time.Since(externalStarted))
		return false, "external port probe failed: " + err.Error()
	}
	a.recordProbeStage(probe, "external_port", true, "none", time.Since(externalStarted))
	return true, ""
}

func (a *Adapter) executeReadinessSandboxProbe(containerID string, probe *Config) (bool, string) {
	if a != nil && a.sandboxProbe != nil {
		return a.sandboxProbe(containerID, probe)
	}
	return a.ExecuteSandboxdProbe(containerID, probe)
}

func (a *Adapter) recordProbeStage(probe *Config, stage string, ok bool, errorClass string, duration time.Duration) {
	if a == nil || a.observer == nil {
		return
	}
	result := "error"
	if ok {
		result = "ok"
		errorClass = "none"
	}
	a.observer.RecordReadinessProbeStage(classifyProbeType(probe), stage, result, errorClass, duration)
}

func classifyProbeFailure(detail string) string {
	detail = strings.ToLower(strings.TrimSpace(detail))
	switch {
	case detail == "":
		return "probe_failed"
	case strings.Contains(detail, "timeout"), strings.Contains(detail, "deadline exceeded"):
		return "timeout"
	case strings.Contains(detail, "connection refused"):
		return "connection_refused"
	case strings.Contains(detail, "no route to host"), strings.Contains(detail, "network is unreachable"):
		return "network_unreachable"
	case strings.Contains(detail, "sandbox access"), strings.Contains(detail, "capability"):
		return "sandbox_access"
	case strings.Contains(detail, "must be positive"), strings.Contains(detail, "target is required"):
		return "invalid_probe"
	default:
		return "probe_failed"
	}
}

func (a *Adapter) TargetState(containerID string) (commonv1.AllocationStatus, bool) {
	if a == nil || a.getContainer == nil {
		return commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED, false
	}
	c, err := a.getContainer(containerID)
	if err != nil || c == nil || c.Status == nil {
		return commonv1.AllocationStatus_ALLOCATION_STATUS_UNSPECIFIED, false
	}
	status := c.Status.Get()
	switch status.State() {
	case runtimeapi.ContainerState_CONTAINER_RUNNING:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true
	case runtimeapi.ContainerState_CONTAINER_EXITED:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, true
	default:
		return commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, true
	}
}

func (a *Adapter) ExecuteSandboxdProbe(containerID string, probe *Config) (bool, string) {
	if probe == nil {
		return true, ""
	}
	if probe.HTTP == nil && probe.TCP == nil {
		return true, ""
	}
	timeout := time.Duration(probe.TimeoutMilliseconds) * time.Millisecond
	if timeout <= 0 {
		timeout = a.defaultExecutionTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	access := a.sandboxAccessor()
	if access == nil {
		return false, "sandbox access unavailable"
	}
	client, err := access.ClientForCapability(ctx, containerID, sandboxaccess.CapabilityProbe)
	if err != nil {
		return false, err.Error()
	}
	response, err := client.Probe(ctx, RequestFromConfig(probe))
	if err != nil {
		return false, access.OperationError(ctx, containerID, sandboxaccess.CapabilityProbe, "execute", err).Error()
	}
	if response.OK {
		return true, ""
	}
	if response.Detail != "" {
		return false, response.Detail
	}
	if response.Target != "" {
		return false, response.Kind + " probe failed for " + response.Target
	}
	return false, response.Kind + " probe failed"
}

func probePort(probe *Config) int32 {
	if probe == nil {
		return 0
	}
	if probe.HTTP != nil {
		return probe.HTTP.Port
	}
	if probe.TCP != nil {
		return probe.TCP.Port
	}
	return 0
}

func (a *Adapter) HandleLivenessFailure(containerID string, attempt int64, detail string) {
	if a == nil {
		return
	}
	message := LivenessFailureMessage(detail, a.diagnosticsFailureDetail(containerID))
	if a.stopReadiness != nil {
		a.stopReadiness(containerID)
	}
	if a.stopLiveness != nil {
		a.stopLiveness(containerID)
	}
	if a.report != nil {
		a.report(containerID, attempt, commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED, 0, false, false, "", message, a.now())
	}
	if a.cleanupFailedStart == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.livenessFailureTimeout)
	defer cancel()
	a.cleanupFailedStart(ctx, containerID)
}

func LivenessFailureMessage(detail string, diagnosticsDetail string) string {
	message := strings.TrimSpace(detail)
	if message == "" {
		message = "liveness probe failed"
	}
	if !strings.HasPrefix(strings.ToLower(message), "liveness probe failed") {
		message = fmt.Sprintf("liveness probe failed: %s", message)
	}
	diagnosticsDetail = strings.TrimSpace(diagnosticsDetail)
	if diagnosticsDetail != "" {
		message = message + "; sandboxd " + diagnosticsDetail
	}
	return message
}

func (a *Adapter) diagnosticsFailureDetail(containerID string) string {
	access := a.sandboxAccessor()
	if access == nil {
		return ""
	}
	return access.DiagnosticsFailureDetail(context.Background(), containerID)
}

func (a *Adapter) sandboxAccessor() *sandboxaccess.Accessor {
	if a == nil || a.sandboxAccess == nil {
		return nil
	}
	return a.sandboxAccess()
}
