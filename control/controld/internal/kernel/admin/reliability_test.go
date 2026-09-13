package adminkernel

import (
	"testing"
	"time"

	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
)

func TestBuildReliabilityHealthOK(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.EmptyHealthSnapshot(),
		time.Minute,
		NodeFleetHealth{},
		time.Now(),
	)
	if health.Status != ReliabilityStatusOK {
		t.Fatalf("status = %q, want ok", health.Status)
	}
	if len(health.Signals) != 0 {
		t.Fatalf("signals = %+v, want empty", health.Signals)
	}
}

func TestBuildReliabilityHealthKeepsRecoveredErrorAsDiagnosticOnly(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.HealthSnapshot{Components: []reconcilekernel.ComponentHealth{{
			Component:           reconcilekernel.ComponentRun,
			LastError:           "claim lost",
			ConsecutiveFailures: 0,
		}}},
		time.Minute,
		NodeFleetHealth{},
		time.Now(),
	)
	if health.Status != ReliabilityStatusOK || health.ReconcileUnhealthyComponents != 0 || len(health.Signals) != 0 {
		t.Fatalf("health = %+v, want recovered component to remain diagnostic without degrading current health", health)
	}
}

func TestBuildReliabilityHealthDegradesOnStuckReconcile(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Minute)
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.HealthSnapshot{Components: []reconcilekernel.ComponentHealth{{
			Component: reconcilekernel.ComponentNode, Running: true, RunningSince: &started,
		}}},
		30*time.Second,
		NodeFleetHealth{},
		now,
	)
	if health.Status != ReliabilityStatusDegraded || health.ReconcileUnhealthyComponents != 1 || len(health.Signals) != 1 {
		t.Fatalf("health = %+v, want one stuck reconcile signal", health)
	}
	if got := health.Signals[0].Message; got != "1 reconcile component(s) are currently failing or stuck (1 stuck)" {
		t.Fatalf("signal = %q", got)
	}
}

func TestBuildReliabilityHealthDegraded(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, []consistencykernel.Issue{{
			Code:     consistencykernel.IssueActiveReservationOnReleasedAllocation,
			Severity: consistencykernel.SeverityError,
		}}, false),
		AllocationLifecycleRetryCounts{Total: 2, Due: 1},
		reconcilekernel.HealthSnapshot{Components: []reconcilekernel.ComponentHealth{{
			Component:           reconcilekernel.ComponentRun,
			LastError:           "database unavailable",
			ConsecutiveFailures: 1,
		}}},
		time.Minute,
		NodeFleetHealth{},
		time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if len(health.Signals) != 3 {
		t.Fatalf("signals = %+v, want 3", health.Signals)
	}
	if health.ReconcileUnhealthyComponents != 1 {
		t.Fatalf("unhealthy reconcile components = %d, want 1", health.ReconcileUnhealthyComponents)
	}

}

func TestBuildReliabilityHealthIncludesNodeFleetFailures(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.EmptyHealthSnapshot(),
		time.Minute,
		NodeFleetHealth{Observed: true, ActiveNodes: 3, ReadyNodes: 1, StaleHeartbeatNodes: 1, StaleSummaryNodes: 1, NotReadyNodes: 1},
		time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded || len(health.Signals) != 1 || health.Signals[0].Code != ReliabilitySignalNodeFleet {
		t.Fatalf("health = %+v", health)
	}
}

func TestBuildReliabilityHealthIncludesUnavailableNodeFleet(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{}, reconcilekernel.EmptyHealthSnapshot(), time.Minute,
		NodeFleetHealth{Observed: true, Unavailable: true, Error: "database unavailable"}, time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded || len(health.Signals) != 1 || health.Signals[0].Code != ReliabilitySignalNodeFleet {
		t.Fatalf("health = %+v", health)
	}
	if health.Signals[0].Message != "node fleet health is unavailable: database unavailable" {
		t.Fatalf("message = %q", health.Signals[0].Message)
	}
}
