package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	privateadminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/admin/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderAllocationLifecycleRetryTableHandlesMissingNextRunAt(t *testing.T) {
	var out bytes.Buffer
	RenderAllocationLifecycleRetryTable(&out, []*privateadminv1.AllocationLifecycleRetry{{
		AllocationID:      "alloc-a",
		RunID:             "run-a",
		Reason:            privateadminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		NodeID:            "node-a",
		ReconcileAttempts: 2,
		LastError:         "node unavailable",
	}})
	got := out.String()
	for _, want := range []string{"ALLOCATION", "alloc-a", "run", "create", "-"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered table %q does not contain %q", got, want)
		}
	}
}

func TestNewAllocationLifecycleRetryJSON(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := NewAllocationLifecycleRetryJSON(&privateadminv1.AllocationLifecycleRetry{
		AllocationID:      "alloc-a",
		RunID:             "run-a",
		Reason:            privateadminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE,
		NodeID:            "node-a",
		ReconcileAttempts: 3,
		NextRunAt:         timestamppb.New(now),
		Due:               true,
		Clearable:         true,
	})
	if got == nil || got.RunID != "run-a" || got.Reason != "delete" || got.NextRunAt != "2026-05-10T12:00:00Z" || !got.Due || !got.Clearable {
		t.Fatalf("NewAllocationLifecycleRetryJSON() = %+v", got)
	}
}

func TestNewAdminAuditEventJSON(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := NewAdminAuditEventJSON(&adminv1.AdminAuditEvent{
		EventID:        "evt-a",
		Operation:      adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY,
		TargetType:     adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION,
		TargetID:       "alloc-a",
		OperatorReason: "manual retry",
		CreatedAt:      timestamppb.New(now),
	})
	if got == nil || got.Operation != "force-allocation-lifecycle-retry" || got.TargetType != "allocation" || got.CreatedAt != "2026-05-10T12:00:00Z" {
		t.Fatalf("NewAdminAuditEventJSON() = %+v", got)
	}
}

func TestRenderAdminAuditEventTable(t *testing.T) {
	var out bytes.Buffer
	RenderAdminAuditEventTable(&out, []*adminv1.AdminAuditEvent{{
		Operation:      adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FAIL_ALLOCATION_LIFECYCLE_RETRY,
		TargetType:     adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION,
		TargetID:       "alloc-a",
		OperatorReason: "operator marked failed",
	}})
	got := out.String()
	for _, want := range []string{"OPERATION", "fail-allocation-lifecycle-retry", "allocation", "alloc-a"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered table %q does not contain %q", got, want)
		}
	}
}

func TestNewConsistencySnapshotJSON(t *testing.T) {
	got := NewConsistencySnapshotJSON(&adminv1.ConsistencySnapshot{
		Status: adminv1.ConsistencyStatus_CONSISTENCY_STATUS_INCONSISTENT,
		Counts: &adminv1.ConsistencyCounts{
			ActiveReservations:         1,
			AllocationLifecycleRetries: 2,
			Issues:                     1,
		},
		Issues: []*adminv1.ConsistencyIssue{{
			Code:             adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_RESERVATION_ON_RELEASED_ALLOCATION,
			Severity:         adminv1.ConsistencyIssueSeverity_CONSISTENCY_ISSUE_SEVERITY_ERROR,
			AllocationID:     "alloc-a",
			RepairOwner:      adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_RUN_CONTROLLER,
			RepairAction:     adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_RUN_CLEANUP,
			RepairTargetType: adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_ALLOCATION,
			RepairTargetID:   "alloc-a",
			AutomaticRepair:  false,
		}},
		Truncated: true,
	})
	if got == nil || got.Status != "inconsistent" || got.Counts.Issues != 1 || len(got.Issues) != 1 || got.Issues[0].Code != "active-reservation-on-released-allocation" || got.Issues[0].RepairOwner != "run-controller" || got.Issues[0].RepairAction != "run-cleanup" || got.Issues[0].RepairTargetType != "allocation" || got.Issues[0].RepairTargetID != "alloc-a" || !got.Truncated {
		t.Fatalf("NewConsistencySnapshotJSON() = %+v", got)
	}
}
