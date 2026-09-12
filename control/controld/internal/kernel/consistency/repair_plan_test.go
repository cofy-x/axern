package consistencykernel

import "testing"

func TestRepairPlanForIssue(t *testing.T) {
	for _, tc := range []struct {
		name           string
		issue          Issue
		wantOwner      RepairOwner
		wantAction     RepairAction
		wantTargetType RepairTargetType
		wantTargetID   string
	}{
		{
			name: "reservation missing allocation",
			issue: Issue{
				Code:         IssueActiveReservationMissingAllocation,
				AllocationID: "alloc-a",
			},
			wantOwner:      RepairOwnerAdminOperatorTriage,
			wantAction:     RepairActionAdminTriage,
			wantTargetType: RepairTargetTypeAllocation,
			wantTargetID:   "alloc-a",
		},
		{
			name: "reservation ended allocation",
			issue: Issue{
				Code:         IssueActiveReservationOnEndedAllocation,
				AllocationID: "alloc-b",
				OwnerType:    "run",
				OwnerID:      "run-ended",
			},
			wantOwner:      RepairOwnerWorkloadController,
			wantAction:     RepairActionWorkloadCleanup,
			wantTargetType: RepairTargetTypeRun,
			wantTargetID:   "run-ended",
		},
		{
			name: "reservation mismatch",
			issue: Issue{
				Code:         IssueActiveReservationAllocationMismatch,
				AllocationID: "alloc-c",
				OwnerType:    "run",
				OwnerID:      "run-a",
			},
			wantOwner:      RepairOwnerWorkloadController,
			wantAction:     RepairActionWorkloadCleanupAndReadmit,
			wantTargetType: RepairTargetTypeRun,
			wantTargetID:   "run-a",
		},
		{
			name: "lease issue",
			issue: Issue{
				Code:         IssueActiveLeaseAllocationNodeMismatch,
				AllocationID: "alloc-d",
			},
			wantOwner:      RepairOwnerNodeLifecycle,
			wantAction:     RepairActionNodeLifecycleReconcile,
			wantTargetType: RepairTargetTypeAllocation,
			wantTargetID:   "alloc-d",
		},
		{
			name: "tunnel issue",
			issue: Issue{
				Code:         IssueActiveTunnelOnEndedAllocation,
				AllocationID: "alloc-e",
				DependentID:  "tun-a",
			},
			wantOwner:      RepairOwnerTunnelController,
			wantAction:     RepairActionTunnelLifecycleReconcile,
			wantTargetType: RepairTargetTypeTunnelSession,
			wantTargetID:   "tun-a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RepairPlanForIssue(tc.issue)
			if got.Owner != tc.wantOwner || got.Action != tc.wantAction || got.TargetType != tc.wantTargetType || got.TargetID != tc.wantTargetID || got.Automatic {
				t.Fatalf("RepairPlanForIssue(%+v) = %+v", tc.issue, got)
			}
		})
	}
}
