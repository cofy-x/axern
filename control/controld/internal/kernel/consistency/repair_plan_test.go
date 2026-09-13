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
			name: "reservation released allocation",
			issue: Issue{
				Code:         IssueActiveReservationOnReleasedAllocation,
				AllocationID: "alloc-b",
				RunID:        "run-ended",
			},
			wantOwner:      RepairOwnerRunController,
			wantAction:     RepairActionRunCleanup,
			wantTargetType: RepairTargetTypeRun,
			wantTargetID:   "run-ended",
		},
		{
			name: "lease issue",
			issue: Issue{
				Code:         IssueActiveLeaseOnEndedAllocation,
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
