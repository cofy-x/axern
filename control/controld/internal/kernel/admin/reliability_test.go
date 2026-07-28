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
		StorageBindingHealth{},
		NodeVolumeHealth{},
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
			Component:           reconcilekernel.ComponentAllocation,
			LastError:           "claim lost",
			ConsecutiveFailures: 0,
		}}},
		time.Minute,
		StorageBindingHealth{},
		NodeVolumeHealth{},
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
		StorageBindingHealth{},
		NodeVolumeHealth{},
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
			Code:     consistencykernel.IssueActiveReservationOnEndedAllocation,
			Severity: consistencykernel.SeverityError,
		}}, false),
		AllocationLifecycleRetryCounts{Total: 2, Due: 1},
		reconcilekernel.HealthSnapshot{Components: []reconcilekernel.ComponentHealth{{
			Component:           reconcilekernel.ComponentRun,
			LastError:           "database unavailable",
			ConsecutiveFailures: 1,
		}}},
		time.Minute,
		StorageBindingHealth{FailedBindings: 3, ReleasingBindings: 4, StuckReleasingBindings: 2, InconsistentClaims: 5, InvalidBindings: 6},
		NodeVolumeHealth{},
		NodeFleetHealth{},
		time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if len(health.Signals) != 4 {
		t.Fatalf("signals = %+v, want 4", health.Signals)
	}
	if health.ReconcileUnhealthyComponents != 1 {
		t.Fatalf("unhealthy reconcile components = %d, want 1", health.ReconcileUnhealthyComponents)
	}
	if health.StorageBindingHealth.FailedBindings != 3 || health.StorageBindingHealth.StuckReleasingBindings != 2 || health.StorageBindingHealth.InconsistentClaims != 5 || health.StorageBindingHealth.InvalidBindings != 6 {
		t.Fatalf("storage binding health = %+v", health.StorageBindingHealth)
	}
	if got := health.Signals[3].Message; got != "3 failed storage binding(s), 2 stuck releasing; list failed bindings and retry after fixing the node/storage cause; inspect release observations for stuck bindings; storage consistency has 5 inconsistent claim(s), 6 invalid binding(s)" {
		t.Fatalf("storage binding signal message = %q", got)
	}
}

func TestBuildReliabilityHealthDegradesOnStorageConsistencyOnly(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.EmptyHealthSnapshot(),
		time.Minute,
		StorageBindingHealth{InconsistentClaims: 1},
		NodeVolumeHealth{},
		NodeFleetHealth{},
		time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded || len(health.Signals) != 1 {
		t.Fatalf("health = %+v, want degraded storage consistency signal", health)
	}
}

func TestBuildReliabilityHealthAllowsTransientReleasingStorageBindings(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.EmptyHealthSnapshot(),
		time.Minute,
		StorageBindingHealth{ReleasingBindings: 4},
		NodeVolumeHealth{},
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

func TestBuildReliabilityHealthIncludesNodeVolumeFailures(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.EmptyHealthSnapshot(),
		time.Minute,
		StorageBindingHealth{},
		NodeVolumeHealth{UnhealthyNodes: 2, PublishedVolumes: 3, LastReconcileStaleAllocations: 1, LastReconcileInvalidVolumes: 1, Error: "last reconcile failed"},
		NodeFleetHealth{},
		time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded {
		t.Fatalf("status = %q, want degraded", health.Status)
	}
	if len(health.Signals) != 1 || health.Signals[0].Code != ReliabilitySignalNodeVolumeManagers {
		t.Fatalf("signals = %+v, want node volume manager signal", health.Signals)
	}
	if health.Signals[0].Message != "2 node(s) report unhealthy volume manager (3 published volume(s), last reconcile: 1 stale allocation(s), 1 invalid volume(s)): last reconcile failed; inspect node volumed health before retrying affected bindings" {
		t.Fatalf("node volume signal message = %q", health.Signals[0].Message)
	}
	if health.NodeVolumeHealth.UnhealthyNodes != 2 ||
		health.NodeVolumeHealth.PublishedVolumes != 3 ||
		health.NodeVolumeHealth.LastReconcileStaleAllocations != 1 ||
		health.NodeVolumeHealth.LastReconcileInvalidVolumes != 1 ||
		health.NodeVolumeHealth.Error != "last reconcile failed" {
		t.Fatalf("node volume health = %+v", health.NodeVolumeHealth)
	}
}

func TestBuildReliabilityHealthIncludesNodeFleetFailures(t *testing.T) {
	health := BuildReliabilityHealth(
		consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
		AllocationLifecycleRetryCounts{},
		reconcilekernel.EmptyHealthSnapshot(),
		time.Minute,
		StorageBindingHealth{},
		NodeVolumeHealth{},
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
		StorageBindingHealth{}, NodeVolumeHealth{}, NodeFleetHealth{Observed: true, Unavailable: true, Error: "database unavailable"}, time.Now(),
	)
	if health.Status != ReliabilityStatusDegraded || len(health.Signals) != 1 || health.Signals[0].Code != ReliabilitySignalNodeFleet {
		t.Fatalf("health = %+v", health)
	}
	if health.Signals[0].Message != "node fleet health is unavailable: database unavailable" {
		t.Fatalf("message = %q", health.Signals[0].Message)
	}
}
