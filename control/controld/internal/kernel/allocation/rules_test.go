package allocationkernel

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"
)

func TestAcceptsObservationRejectsEndedAndWrongNode(t *testing.T) {
	obs := &nodev1.AllocationLifecycleObservation{
		AllocationID: "alloc-a",
		State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
	}
	if !AcceptsObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND, "node-a", "node-a", obs) {
		t.Fatal("expected current observation to be accepted")
	}
	if AcceptsObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, "node-a", "node-a", obs) {
		t.Fatal("expected ended allocation to reject active observation")
	}
	if AcceptsObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING, "node-a", "node-a", obs) {
		t.Fatal("expected releasing allocation to reject observation")
	}
	if AcceptsObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_BOUND, "node-a", "node-b", obs) {
		t.Fatal("expected observation from another node to be rejected")
	}
	starting := &nodev1.AllocationLifecycleObservation{AllocationID: "alloc-a", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING}
	if AcceptsObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, "node-a", "node-a", starting) {
		t.Fatal("expected stale STARTING observation not to regress ACTIVE allocation")
	}
}

func TestValidateObservationBindingRequiresExactNonEmptyNode(t *testing.T) {
	if err := ValidateObservationBinding(" node-a ", "node-a"); err != nil {
		t.Fatalf("ValidateObservationBinding() error = %v", err)
	}
	for _, reportingNode := range []string{"", "node-b"} {
		if err := ValidateObservationBinding("node-a", reportingNode); err == nil {
			t.Fatalf("ValidateObservationBinding(node-a, %q) succeeded", reportingNode)
		}
	}
}

func TestExpectedInNodeInventoryAt(t *testing.T) {
	snapshotAt := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	if !ExpectedInNodeInventoryAt(snapshotAt.Add(-time.Second), snapshotAt) {
		t.Fatal("expected node-active allocation before snapshot to require inventory presence")
	}
	if !ExpectedInNodeInventoryAt(snapshotAt, snapshotAt) {
		t.Fatal("expected node-active allocation at snapshot to require inventory presence")
	}
	if ExpectedInNodeInventoryAt(snapshotAt.Add(time.Second), snapshotAt) {
		t.Fatal("expected node-active allocation after snapshot to be ignored")
	}
	if ExpectedInNodeInventoryAt(time.Time{}, snapshotAt) {
		t.Fatal("expected allocation without node-active time to be ignored")
	}
}

func TestMissingFromNodeInventory(t *testing.T) {
	snapshotAt := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	got := MissingFromNodeInventory(NodeInventorySnapshot{
		ActiveAllocationIDs: []string{"alloc-active", " "},
		CollectedAt:         snapshotAt,
	}, []NodeInventoryExpectation{
		{AllocationID: "alloc-active", NodeActiveAt: snapshotAt.Add(-time.Second)},
		{AllocationID: "alloc-missing", NodeActiveAt: snapshotAt.Add(-time.Second)},
		{AllocationID: "alloc-too-new", NodeActiveAt: snapshotAt.Add(time.Second)},
		{AllocationID: "alloc-not-active-yet"},
	})
	if len(got) != 1 {
		t.Fatalf("missing = %#v, want one allocation", got)
	}
	if got[0].AllocationID != "alloc-missing" {
		t.Fatalf("missing[0] = %#v, want alloc-missing", got[0])
	}
}

func TestRunStatusFromObservation(t *testing.T) {
	exitZero := int32(0)
	if got := RunStatusFromObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, &exitZero, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED); got != runv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("exit 0 mapped to %s", got)
	}
	exitOne := int32(1)
	if got := RunStatusFromObservation(commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED, &exitOne, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED); got != runv1.RunStatus_RUN_STATUS_FAILED {
		t.Fatalf("exit 1 mapped to %s", got)
	}
}

func TestEvaluateLifecycleRetryClearance(t *testing.T) {
	tests := []struct {
		name       string
		in         LifecycleRetryClearanceInput
		clearable  bool
		blockedFor string
	}{
		{
			name: "active allocation blocks clear",
			in: LifecycleRetryClearanceInput{
				AllocationState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE.String(),
			},
			blockedFor: "allocation lifecycle state is ALLOCATION_LIFECYCLE_STATE_ACTIVE",
		},
		{
			name: "active access grant blocks terminal allocation",
			in: LifecycleRetryClearanceInput{
				AllocationState:      commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(),
				HasActiveAccessGrant: true,
			},
			blockedFor: "active allocation access grants",
		},
		{
			name: "active tunnel blocks terminal allocation",
			in: LifecycleRetryClearanceInput{
				AllocationState:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(),
				HasActiveTunnelSession: true,
			},
			blockedFor: "active tunnel sessions",
		},
		{
			name: "nonterminal run blocks clear",
			in: LifecycleRetryClearanceInput{
				AllocationState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(),
				RunStatus:       runv1.RunStatus_RUN_STATUS_RUNNING.String(),
			},
			blockedFor: "run status is RUN_STATUS_RUNNING",
		},
		{
			name: "terminal run is clearable",
			in: LifecycleRetryClearanceInput{
				AllocationState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(),
				RunStatus:       runv1.RunStatus_RUN_STATUS_FAILED.String(),
			},
			clearable: true,
		},
		{
			name: "missing run is clearable",
			in: LifecycleRetryClearanceInput{
				AllocationState: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String(),
			},
			clearable: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateLifecycleRetryClearance(tc.in)
			if got.Clearable != tc.clearable || got.BlockedReason != tc.blockedFor {
				t.Fatalf("clearance = %#v, want clearable=%v blocked=%q", got, tc.clearable, tc.blockedFor)
			}
		})
	}
}

func TestScheduleCreateRetryRequest(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	req, ok := ScheduleCreateRetryRequest("alloc-a", 0, "node unavailable", now)
	if !ok {
		t.Fatal("ScheduleCreateRetryRequest returned ok=false for first failure")
	}
	if req.AllocationID != "alloc-a" || req.Intent != ReconcileIntentEnsurePresent || req.LastReconcileError != "node unavailable" || !req.IncrementAttempts {
		t.Fatalf("request = %#v, want create retry request for alloc-a", req)
	}
	if want := now.Add(CreateRetryDelay(1)); !req.NextRunAt.Equal(want) {
		t.Fatalf("NextRunAt = %v, want %v", req.NextRunAt, want)
	}

	req, ok = ScheduleCreateRetryRequest("alloc-a", CreateRetryMaxAttempts-1, "node unavailable", now)
	if ok {
		t.Fatalf("ScheduleCreateRetryRequest at exhaustion returned ok=true with request %#v", req)
	}
}

func TestScheduleDeleteRetryRequest(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	req := ScheduleDeleteRetryRequest("alloc-a", "node unavailable", now)
	if req.AllocationID != "alloc-a" || req.Intent != ReconcileIntentEnsureAbsent || req.LastReconcileError != "node unavailable" || !req.IncrementAttempts {
		t.Fatalf("request = %#v, want delete retry request for alloc-a", req)
	}
	if want := now.Add(DeleteRetryDelay); !req.NextRunAt.Equal(want) {
		t.Fatalf("NextRunAt = %v, want %v", req.NextRunAt, want)
	}

	immediate := ScheduleDeleteRequest("alloc-a", now)
	if immediate.AllocationID != "alloc-a" || immediate.Intent != ReconcileIntentEnsureAbsent || immediate.LastReconcileError != "" || immediate.IncrementAttempts {
		t.Fatalf("immediate request = %#v, want fresh delete intent for alloc-a", immediate)
	}
}
