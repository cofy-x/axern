package service

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/probes"
)

func (h *sandboxService) configureProbeCoordinator() {
	h.probeAdapter = probes.NewAdapter(probes.AdapterOptions{
		GetContainer: func(containerID string) (*container.Container, error) {
			if h.containerManager == nil {
				return nil, fmt.Errorf("container manager unavailable")
			}
			return h.containerManager.Get(containerID)
		},
		ExternalPortProbe: func(ctx context.Context, allocationID string, port int32) error {
			return h.sandboxNetworking().ProbePort(ctx, allocationID, port)
		},
		Observer:      probeMetricsObserver{},
		SandboxAccess: h.sandboxAccessor,
		StopReadiness: h.stopReadinessProbe,
		StopLiveness:  h.stopLivenessProbe,
		Report:        h.ReportAllocationStatus,
		CleanupFailedStart: func(ctx context.Context, allocationID string) {
			h.allocationController().CleanupFailedStart(ctx, allocationID)
		},
	})
	h.probeCoordinator = probes.NewCoordinator(probes.Options{
		Report:                h.ReportAllocationStatus,
		TargetState:           h.probeAdapter.TargetState,
		Execute:               h.probeAdapter.ExecuteSandboxdProbe,
		ExecuteReadiness:      h.probeAdapter.ExecuteReadinessProbe,
		HandleLivenessFailure: h.probeAdapter.HandleLivenessFailure,
		Observer:              probeMetricsObserver{},
	})
}

type probeMetricsObserver struct{}

func (probeMetricsObserver) RecordProbeAttempt(kind, probeType, result string, duration time.Duration) {
	metrics.RecordProbeAttemptDuration(kind, probeType, result, duration.Seconds())
}

func (probeMetricsObserver) RecordReadinessProbeStage(probeType, stage, result, errorClass string, duration time.Duration) {
	metrics.RecordReadinessProbeStageDuration(probeType, stage, result, errorClass, duration.Seconds())
}

func (probeMetricsObserver) RecordReadinessWait(probeType, result string, duration time.Duration) {
	metrics.RecordReadinessWaitDuration(probeType, result, duration.Seconds())
}

func (h *sandboxService) stopReadinessProbe(containerID string) {
	if h == nil || h.probeCoordinator == nil {
		return
	}
	h.probeCoordinator.StopReadiness(containerID)
}

func (h *sandboxService) stopLivenessProbe(containerID string) {
	if h == nil || h.probeCoordinator == nil {
		return
	}
	h.probeCoordinator.StopLiveness(containerID)
}

func (h *sandboxService) stopAllReadinessWorkers() {
	if h == nil || h.probeCoordinator == nil {
		return
	}
	h.probeCoordinator.StopAllReadiness()
}

func (h *sandboxService) stopAllLivenessWorkers() {
	if h == nil || h.probeCoordinator == nil {
		return
	}
	h.probeCoordinator.StopAllLiveness()
}
