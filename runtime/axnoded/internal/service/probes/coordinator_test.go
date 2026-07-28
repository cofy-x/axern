package probes

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

type probeObservation struct {
	kind       string
	probeType  string
	stage      string
	result     string
	errorClass string
	duration   time.Duration
}

type fakeProbeObserver struct {
	attempts chan probeObservation
	waits    chan probeObservation
	stages   chan probeObservation
}

func (f fakeProbeObserver) RecordProbeAttempt(kind, probeType, result string, duration time.Duration) {
	f.attempts <- probeObservation{kind: kind, probeType: probeType, result: result, duration: duration}
}

func (f fakeProbeObserver) RecordReadinessProbeStage(probeType, stage, result, errorClass string, duration time.Duration) {
	if f.stages != nil {
		f.stages <- probeObservation{probeType: probeType, stage: stage, result: result, errorClass: errorClass, duration: duration}
	}
}

func (f fakeProbeObserver) RecordReadinessWait(probeType, result string, duration time.Duration) {
	f.waits <- probeObservation{probeType: probeType, result: result, duration: duration}
}

func TestReadinessGateRequiresSuccessfulReadinessWhenConfigured(t *testing.T) {
	c := NewCoordinator(Options{})

	if c.RequiresReadinessGate("alloc-1", &Config{}) {
		t.Fatal("RequiresReadinessGate = true without readiness configuration")
	}
	if !c.ReadinessGateSatisfied("alloc-1") {
		t.Fatal("ReadinessGateSatisfied = false without readiness configuration")
	}

	c.ResetReadinessGate("alloc-1")
	if !c.RequiresReadinessGate("alloc-1", &Config{}) {
		t.Fatal("RequiresReadinessGate = false after readiness configuration")
	}
	if c.ReadinessGateSatisfied("alloc-1") {
		t.Fatal("ReadinessGateSatisfied = true before first readiness success")
	}

	c.MarkReadinessGateSatisfied("alloc-1")
	if !c.ReadinessGateSatisfied("alloc-1") {
		t.Fatal("ReadinessGateSatisfied = false after readiness success")
	}
}

func TestStartReadinessWithoutProbeDoesNotBlockLiveness(t *testing.T) {
	c := NewCoordinator(Options{})
	c.ResetReadinessGate("alloc-1")

	c.StartReadiness("alloc-1", 1, nil)

	if c.RequiresReadinessGate("alloc-1", &Config{}) {
		t.Fatal("RequiresReadinessGate = true after readiness probe cleared")
	}
	if !c.ReadinessGateSatisfied("alloc-1") {
		t.Fatal("ReadinessGateSatisfied = false after readiness probe cleared")
	}
}

func TestStopLivenessDoesNotClearReadinessGate(t *testing.T) {
	c := NewCoordinator(Options{})
	c.ResetReadinessGate("alloc-1")

	c.StopLiveness("alloc-1")

	if !c.RequiresReadinessGate("alloc-1", &Config{}) {
		t.Fatal("RequiresReadinessGate = false after stopping liveness")
	}
	if c.ReadinessGateSatisfied("alloc-1") {
		t.Fatal("ReadinessGateSatisfied = true before readiness success")
	}
}

func TestRunUsesReadinessExecutorOnlyForReadiness(t *testing.T) {
	readinessCalls := 0
	livenessCalls := 0
	reports := make(chan bool, 1)
	c := NewCoordinator(Options{
		TargetState: func(string) (commonv1.AllocationStatus, bool) {
			return commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true
		},
		Execute: func(string, *Config) (bool, string) {
			livenessCalls++
			return true, ""
		},
		ExecuteReadiness: func(string, *Config) (bool, string) {
			readinessCalls++
			return true, ""
		},
		Report: func(_ string, _ int64, _ commonv1.AllocationStatus, _ int32, _ bool, ready bool, _ string, _ string, _ time.Time) {
			if ready {
				reports <- true
			}
		},
	})

	c.StartReadiness("alloc-1", 1, &Config{TCP: &TCPConfig{Port: 8080}, PeriodMilliseconds: 1000})
	select {
	case <-reports:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness report")
	}
	c.StopReadiness("alloc-1")

	if readinessCalls == 0 {
		t.Fatal("readiness executor was not called")
	}
	if livenessCalls != 0 {
		t.Fatalf("generic executor calls during readiness = %d, want 0", livenessCalls)
	}
}

func TestReadinessMetricsRecordFirstReady(t *testing.T) {
	observer := fakeProbeObserver{
		attempts: make(chan probeObservation, 1),
		waits:    make(chan probeObservation, 1),
	}
	reports := make(chan bool, 1)
	c := NewCoordinator(Options{
		TargetState: func(string) (commonv1.AllocationStatus, bool) {
			return commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, true
		},
		ExecuteReadiness: func(string, *Config) (bool, string) {
			return true, ""
		},
		Report: func(_ string, _ int64, _ commonv1.AllocationStatus, _ int32, _ bool, ready bool, _ string, _ string, _ time.Time) {
			if ready {
				reports <- true
			}
		},
		Observer: observer,
	})

	c.StartReadiness("alloc-1", 1, &Config{HTTP: &HTTPConfig{Port: 8080, Path: "/"}, PeriodMilliseconds: 1000})
	defer c.StopReadiness("alloc-1")

	select {
	case <-reports:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness report")
	}

	select {
	case observed := <-observer.attempts:
		if observed.kind != "readiness" || observed.probeType != "http" || observed.result != "ok" {
			t.Fatalf("attempt observation = %#v", observed)
		}
		if observed.duration < 0 {
			t.Fatalf("attempt duration = %s, want non-negative", observed.duration)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for probe attempt observation")
	}

	select {
	case observed := <-observer.waits:
		if observed.probeType != "http" || observed.result != "ok" {
			t.Fatalf("wait observation = %#v", observed)
		}
		if observed.duration < 0 {
			t.Fatalf("wait duration = %s, want non-negative", observed.duration)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readiness wait observation")
	}
}

func TestRequestFromConfigUsesSandboxLocalTarget(t *testing.T) {
	request := RequestFromConfig(&Config{
		HTTP:                &HTTPConfig{Port: 8080, Path: "healthz", Scheme: "http"},
		TimeoutMilliseconds: 3000,
	})
	if request.Host != "" {
		t.Fatalf("probe host = %q, want sandboxd default", request.Host)
	}
	if request.TimeoutMS != 3000 || request.HTTP == nil || request.HTTP.Port != 8080 || request.HTTP.Path != "healthz" {
		t.Fatalf("probe request = %#v", request)
	}
}
