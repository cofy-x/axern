package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRenderAllocationLifecycleRetryTableHandlesMissingNextRunAt(t *testing.T) {
	var out bytes.Buffer
	RenderAllocationLifecycleRetryTable(&out, []*adminv1.AllocationLifecycleRetry{{
		AllocationID:      "alloc-a",
		OwnerType:         adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_RUN,
		OwnerID:           "run-a",
		Reason:            adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		NodeID:            "node-a",
		Attempt:           1,
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
	got := NewAllocationLifecycleRetryJSON(&adminv1.AllocationLifecycleRetry{
		AllocationID:      "alloc-a",
		OwnerType:         adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE,
		OwnerID:           "svc-a",
		Reason:            adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_DELETE,
		NodeID:            "node-a",
		ReconcileAttempts: 3,
		NextRunAt:         timestamppb.New(now),
		Due:               true,
		Clearable:         true,
	})
	if got == nil || got.OwnerType != "service" || got.Reason != "delete" || got.NextRunAt != "2026-05-10T12:00:00Z" || !got.Due || !got.Clearable {
		t.Fatalf("NewAllocationLifecycleRetryJSON() = %+v", got)
	}
}

func TestNewAdminAuditEventJSON(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	got := NewAdminAuditEventJSON(&adminv1.AdminAuditEvent{
		EventID:        "evt-a",
		Operation:      adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_RETRY_STORAGE_BINDING,
		TargetType:     adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_STORAGE_BINDING,
		TargetID:       "bind-a",
		OperatorReason: "manual retry",
		CreatedAt:      timestamppb.New(now),
	})
	if got == nil || got.Operation != "retry-storage-binding" || got.TargetType != "storage-binding" || got.CreatedAt != "2026-05-10T12:00:00Z" {
		t.Fatalf("NewAdminAuditEventJSON() = %+v", got)
	}
}

func TestNewStorageBindingJSON(t *testing.T) {
	now := time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
	got := NewStorageBindingJSON(&adminv1.StorageBinding{
		BindingID:    "bind-1",
		ClaimID:      "claim-1",
		Namespace:    "default",
		ClaimName:    "data",
		WorkloadID:   "svc-1",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Status:       storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
		Message:      "publish failed",
		UpdatedAt:    timestamppb.New(now),
	})
	if got == nil || got.BindingID != "bind-1" || got.Status != "failed" || got.UpdatedAt != "2026-05-12T10:00:00Z" {
		t.Fatalf("NewStorageBindingJSON() = %+v", got)
	}
	if got.WorkloadID != "svc-1" || got.WorkloadType != "service" {
		t.Fatalf("NewStorageBindingJSON() workload = %s/%s, want service/svc-1", got.WorkloadType, got.WorkloadID)
	}
}

func TestRenderStorageBindingTable(t *testing.T) {
	var out bytes.Buffer
	RenderStorageBindingTable(&out, []*adminv1.StorageBinding{{
		BindingID:    "bind-1",
		Namespace:    "default",
		ClaimName:    "data",
		WorkloadID:   "svc-1",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Status:       storagev1.VolumeStatus_VOLUME_STATUS_FAILED,
		Message:      "publish failed",
	}})
	got := out.String()
	for _, want := range []string{"BINDING", "WORKLOAD", "bind-1", "default", "data", "service/svc-1", "failed", "publish failed"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered table %q does not contain %q", got, want)
		}
	}
}

func TestRenderStorageBindingIncludesWorkload(t *testing.T) {
	var out bytes.Buffer
	RenderStorageBinding(&out, &adminv1.StorageBinding{
		BindingID:    "bind-1",
		Namespace:    "default",
		ClaimName:    "data",
		WorkloadID:   "svc-1",
		WorkloadType: "service",
		AllocationID: "alloc-1",
		NodeID:       "node-a",
		Status:       storagev1.VolumeStatus_VOLUME_STATUS_BOUND,
	})
	got := out.String()
	for _, want := range []string{"Binding: bind-1", "Workload: service/svc-1", "Allocation: alloc-1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered binding %q does not contain %q", got, want)
		}
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
			Code:             adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_RESERVATION_ON_ENDED_ALLOCATION,
			Severity:         adminv1.ConsistencyIssueSeverity_CONSISTENCY_ISSUE_SEVERITY_ERROR,
			AllocationID:     "alloc-a",
			RepairOwner:      adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_WORKLOAD_CONTROLLER,
			RepairAction:     adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_WORKLOAD_CLEANUP,
			RepairTargetType: adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_ALLOCATION,
			RepairTargetID:   "alloc-a",
			AutomaticRepair:  false,
		}},
		Truncated: true,
	})
	if got == nil || got.Status != "inconsistent" || got.Counts.Issues != 1 || len(got.Issues) != 1 || got.Issues[0].Code != "active-reservation-on-ended-allocation" || got.Issues[0].RepairOwner != "workload-controller" || got.Issues[0].RepairAction != "workload-cleanup" || got.Issues[0].RepairTargetType != "allocation" || got.Issues[0].RepairTargetID != "alloc-a" || !got.Truncated {
		t.Fatalf("NewConsistencySnapshotJSON() = %+v", got)
	}
}

func TestNewAdminReliabilityHealthJSONIncludesNodeVolumeHealth(t *testing.T) {
	got := NewAdminReliabilityHealthJSON(&adminv1.AdminReliabilityHealth{
		Status: adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_DEGRADED,
		StorageBindingHealth: &adminv1.AdminStorageBindingHealth{
			FailedBindings:         5,
			ReleasingBindings:      6,
			StuckReleasingBindings: 7,
			DeletingClaims:         8,
			StuckDeletingClaims:    9,
			InconsistentClaims:     10,
			InvalidBindings:        11,
		},
		NodeVolumeHealth: &adminv1.AdminNodeVolumeHealth{
			UnhealthyNodes:                1,
			PublishedVolumes:              2,
			LastReconcileStaleAllocations: 3,
			LastReconcileInvalidVolumes:   4,
			Error:                         "volumed unavailable",
		},
		ReconcileComponents: []*adminv1.ReconcileComponentHealth{{
			Component:           "allocation",
			ConsecutiveFailures: 2,
			LastError:           "claim lost",
		}},
		Signals: []*adminv1.AdminReliabilitySignal{{
			Code:    adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_NODE_VOLUME_MANAGERS,
			Message: "node volume manager is unhealthy",
		}},
	})
	if got == nil || got.Status != "degraded" || got.NodeVolumeHealth == nil || got.NodeVolumeHealth.UnhealthyNodes != 1 || got.NodeVolumeHealth.PublishedVolumes != 2 || got.NodeVolumeHealth.LastReconcileStaleAllocations != 3 || got.NodeVolumeHealth.LastReconcileInvalidVolumes != 4 || got.NodeVolumeHealth.Error != "volumed unavailable" {
		t.Fatalf("NewAdminReliabilityHealthJSON() = %+v", got)
	}
	if got.StorageBindingHealth == nil || got.StorageBindingHealth.FailedBindings != 5 || got.StorageBindingHealth.ReleasingBindings != 6 || got.StorageBindingHealth.StuckReleasingBindings != 7 || got.StorageBindingHealth.DeletingClaims != 8 || got.StorageBindingHealth.StuckDeletingClaims != 9 || got.StorageBindingHealth.InconsistentClaims != 10 || got.StorageBindingHealth.InvalidBindings != 11 {
		t.Fatalf("storage binding health = %+v", got.StorageBindingHealth)
	}
	if len(got.Signals) != 1 || got.Signals[0].Code != "node-volume-managers" {
		t.Fatalf("signals = %+v", got.Signals)
	}
	if len(got.ReconcileComponents) != 1 || got.ReconcileComponents[0].Component != "allocation" || got.ReconcileComponents[0].ConsecutiveFailures != 2 || got.ReconcileComponents[0].LastError != "claim lost" {
		t.Fatalf("reconcile components = %+v", got.ReconcileComponents)
	}
}
