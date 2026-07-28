package allocationkernel

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestAcceptsObservationRejectsStaleAndEnded(t *testing.T) {
	obs := &nodev1.AllocationStatusObservation{
		AllocationID: "alloc-a",
		Attempt:      1,
		Status:       commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
	}
	if !AcceptsObservation(commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND, 1, "node-a", "node-a", obs) {
		t.Fatal("expected current observation to be accepted")
	}
	if AcceptsObservation(commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND, 2, "node-a", "node-a", obs) {
		t.Fatal("expected old attempt to be rejected")
	}
	if AcceptsObservation(commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, 1, "node-a", "node-a", obs) {
		t.Fatal("expected ended allocation to reject active observation")
	}
	if AcceptsObservation(commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASING, 1, "node-a", "node-a", obs) {
		t.Fatal("expected releasing allocation to reject observation")
	}
	if AcceptsObservation(commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND, 1, "node-a", "node-b", obs) {
		t.Fatal("expected observation from another node to be rejected")
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
		{AllocationID: "alloc-active", Attempt: 1, NodeActiveAt: snapshotAt.Add(-time.Second)},
		{AllocationID: "alloc-missing", Attempt: 2, NodeActiveAt: snapshotAt.Add(-time.Second)},
		{AllocationID: "alloc-too-new", Attempt: 3, NodeActiveAt: snapshotAt.Add(time.Second)},
		{AllocationID: "alloc-not-active-yet", Attempt: 4},
	})
	if len(got) != 1 {
		t.Fatalf("missing = %#v, want one allocation", got)
	}
	if got[0].AllocationID != "alloc-missing" || got[0].Attempt != 2 {
		t.Fatalf("missing[0] = %#v, want alloc-missing attempt 2", got[0])
	}
}

func TestRunStatusFromAllocation(t *testing.T) {
	if got := RunStatusFromAllocation(commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, 0); got != runv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("exit 0 mapped to %s", got)
	}
	if got := RunStatusFromAllocation(commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED, 1); got != runv1.RunStatus_RUN_STATUS_FAILED {
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
				AllocationStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(),
				OwnerType:        OwnerRun,
			},
			blockedFor: "allocation status is ALLOCATION_STATUS_RUNNING",
		},
		{
			name: "active reservation blocks terminal allocation",
			in: LifecycleRetryClearanceInput{
				AllocationStatus:     commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:            OwnerRun,
				HasActiveReservation: true,
			},
			blockedFor: "active reservations",
		},
		{
			name: "active lease blocks terminal allocation",
			in: LifecycleRetryClearanceInput{
				AllocationStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:        OwnerRun,
				HasActiveLease:   true,
			},
			blockedFor: "active leases",
		},
		{
			name: "active tunnel blocks terminal allocation",
			in: LifecycleRetryClearanceInput{
				AllocationStatus:       commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:              OwnerRun,
				HasActiveTunnelSession: true,
			},
			blockedFor: "active tunnel sessions",
		},
		{
			name: "nonterminal run owner blocks clear",
			in: LifecycleRetryClearanceInput{
				AllocationStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:        OwnerRun,
				OwnerRunStatus:   runv1.RunStatus_RUN_STATUS_RUNNING.String(),
			},
			blockedFor: "owner run status is RUN_STATUS_RUNNING",
		},
		{
			name: "terminal run owner is clearable",
			in: LifecycleRetryClearanceInput{
				AllocationStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:        OwnerRun,
				OwnerRunStatus:   runv1.RunStatus_RUN_STATUS_FAILED.String(),
			},
			clearable: true,
		},
		{
			name: "missing run owner is clearable",
			in: LifecycleRetryClearanceInput{
				AllocationStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:        OwnerRun,
			},
			clearable: true,
		},
		{
			name: "referenced service owner blocks clear",
			in: LifecycleRetryClearanceInput{
				AllocationStatus:                 commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:                        OwnerService,
				OwnerServiceReferencesAllocation: true,
			},
			blockedFor: "owner service still references allocation",
		},
		{
			name: "unreferenced service owner is clearable",
			in: LifecycleRetryClearanceInput{
				AllocationStatus: commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
				OwnerType:        OwnerService,
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
	if req.AllocationID != "alloc-a" || req.Reason != ReconcileReasonCreate || req.LastReconcileError != "node unavailable" || !req.IncrementAttempts {
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
	if req.AllocationID != "alloc-a" || req.Reason != ReconcileReasonDelete || req.LastReconcileError != "node unavailable" || !req.IncrementAttempts {
		t.Fatalf("request = %#v, want delete retry request for alloc-a", req)
	}
	if want := now.Add(DeleteRetryDelay); !req.NextRunAt.Equal(want) {
		t.Fatalf("NextRunAt = %v, want %v", req.NextRunAt, want)
	}

	req = ScheduleImmediateDeleteRetryRequest("alloc-a", "node unavailable", now)
	if req.AllocationID != "alloc-a" || req.Reason != ReconcileReasonDelete || req.LastReconcileError != "node unavailable" || req.IncrementAttempts {
		t.Fatalf("immediate request = %#v, want non-incrementing delete retry request for alloc-a", req)
	}
	if !req.NextRunAt.Equal(now) {
		t.Fatalf("immediate NextRunAt = %v, want %v", req.NextRunAt, now)
	}
}
