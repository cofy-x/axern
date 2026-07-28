package probes

import (
	"context"
	"errors"
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestExecuteReadinessProbeRecordsSandboxAndExternalStages(t *testing.T) {
	observer := fakeProbeObserver{stages: make(chan probeObservation, 2)}
	adapter := NewAdapter(AdapterOptions{
		Observer: observer,
		SandboxProbe: func(string, *Config) (bool, string) {
			return true, ""
		},
		ExternalPortProbe: func(context.Context, string, int32) error {
			return errors.New("i/o timeout")
		},
	})

	ok, _ := adapter.ExecuteReadinessProbe("alloc-123", &Config{
		HTTP:                &HTTPConfig{Port: 8080, Path: "/"},
		TimeoutMilliseconds: 1000,
	})
	if ok {
		t.Fatal("ExecuteReadinessProbe() = true, want false")
	}

	sandbox := <-observer.stages
	if sandbox.stage != "sandbox" || sandbox.result != "ok" || sandbox.errorClass != "none" {
		t.Fatalf("sandbox stage = %#v", sandbox)
	}
	external := <-observer.stages
	if external.stage != "external_port" || external.result != "error" || external.errorClass != "timeout" {
		t.Fatalf("external stage = %#v", external)
	}
}

func TestClassifyProbeFailure(t *testing.T) {
	tests := map[string]string{
		"dial tcp: i/o timeout":            "timeout",
		"connect: connection refused":      "connection_refused",
		"network is unreachable":           "network_unreachable",
		"sandbox access unavailable":       "sandbox_access",
		"http probe port must be positive": "invalid_probe",
		"unexpected":                       "probe_failed",
	}
	for detail, want := range tests {
		if got := classifyProbeFailure(detail); got != want {
			t.Errorf("classifyProbeFailure(%q) = %q, want %q", detail, got, want)
		}
	}
}

func TestLivenessFailureMessageNormalizesDetail(t *testing.T) {
	if got := LivenessFailureMessage("", ""); got != "liveness probe failed" {
		t.Fatalf("empty detail message = %q, want default failure", got)
	}
	if got := LivenessFailureMessage("connection refused", ""); got != "liveness probe failed: connection refused" {
		t.Fatalf("detail message = %q, want prefixed detail", got)
	}
	if got := LivenessFailureMessage("liveness probe failed: timeout", "diagnostics unavailable"); got != "liveness probe failed: timeout; sandboxd diagnostics unavailable" {
		t.Fatalf("diagnostic message = %q, want appended diagnostics", got)
	}
}

func TestAdapterHandleLivenessFailureStopsReportsAndCleansUp(t *testing.T) {
	var stoppedReadiness, stoppedLiveness, cleaned bool
	var reportedStatus commonv1.AllocationStatus
	var reportedMessage string
	adapter := NewAdapter(AdapterOptions{
		StopReadiness: func(containerID string) {
			stoppedReadiness = containerID == "alloc-123"
		},
		StopLiveness: func(containerID string) {
			stoppedLiveness = containerID == "alloc-123"
		},
		Report: func(allocationID string, attempt int64, status commonv1.AllocationStatus, _ int32, _ bool, _ bool, _ string, message string, _ time.Time) {
			if allocationID != "alloc-123" || attempt != 7 {
				t.Fatalf("reported allocation/attempt = %s/%d, want alloc-123/7", allocationID, attempt)
			}
			reportedStatus = status
			reportedMessage = message
		},
		CleanupFailedStart: func(ctx context.Context, allocationID string) {
			if err := ctx.Err(); err != nil {
				t.Fatalf("cleanup context already done: %v", err)
			}
			cleaned = allocationID == "alloc-123"
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC)
		},
	})

	adapter.HandleLivenessFailure("alloc-123", 7, "timeout")

	if !stoppedReadiness || !stoppedLiveness {
		t.Fatalf("stopped readiness/liveness = %v/%v, want true/true", stoppedReadiness, stoppedLiveness)
	}
	if reportedStatus != commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED {
		t.Fatalf("reported status = %v, want FAILED", reportedStatus)
	}
	if reportedMessage != "liveness probe failed: timeout" {
		t.Fatalf("reported message = %q, want normalized timeout", reportedMessage)
	}
	if !cleaned {
		t.Fatal("cleanup was not called for allocation")
	}
}
