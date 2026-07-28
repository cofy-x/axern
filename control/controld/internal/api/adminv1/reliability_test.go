package adminv1

import (
	"context"
	"errors"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCheckConsistencyMapsSnapshot(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	srv := New(Dependencies{
		Now: func() time.Time { return now },
		Reliability: fakeReliability{
			snapshot: consistencykernel.NewSnapshot(consistencykernel.Counts{
				ActiveReservations: 3,
				ActiveLeases:       2,
				ActiveTunnels:      1,
				ReconcileQueue:     4,
			}, []consistencykernel.Issue{{
				Code:         consistencykernel.IssueActiveReservationOnEndedAllocation,
				Severity:     consistencykernel.SeverityError,
				AllocationID: "alloc-a",
				OwnerType:    "service",
				OwnerID:      "svc-a",
				NodeID:       "node-a",
				Status:       "ALLOCATION_STATUS_RELEASED",
				Detail:       "active reservation remains after allocation ended",
			}}, true),
		},
	})

	resp, err := srv.CheckConsistency(context.Background(), &adminv1.CheckConsistencyRequest{})
	if err != nil {
		t.Fatalf("CheckConsistency() error = %v", err)
	}
	got := resp.GetSnapshot()
	if got.GetStatus() != adminv1.ConsistencyStatus_CONSISTENCY_STATUS_INCONSISTENT || !got.GetTruncated() {
		t.Fatalf("snapshot status/truncated = %s/%v", got.GetStatus(), got.GetTruncated())
	}
	if got.GetCounts().GetActiveReservations() != 3 || got.GetCounts().GetAllocationLifecycleRetries() != 4 || got.GetCounts().GetIssues() != 1 {
		t.Fatalf("counts = %+v", got.GetCounts())
	}
	if len(got.GetIssues()) != 1 || got.GetIssues()[0].GetCode() != adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_ACTIVE_RESERVATION_ON_ENDED_ALLOCATION {
		t.Fatalf("issues = %+v", got.GetIssues())
	}
	issue := got.GetIssues()[0]
	if issue.GetRepairOwner() != adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_WORKLOAD_CONTROLLER ||
		issue.GetRepairAction() != adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_WORKLOAD_CLEANUP ||
		issue.GetRepairTargetType() != adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_SERVICE ||
		issue.GetRepairTargetID() != "svc-a" ||
		issue.GetAutomaticRepair() {
		t.Fatalf("issue repair plan = owner:%s action:%s target:%s/%s automatic:%v", issue.GetRepairOwner(), issue.GetRepairAction(), issue.GetRepairTargetType(), issue.GetRepairTargetID(), issue.GetAutomaticRepair())
	}
}

func TestGetAdminReliabilityHealthMapsDegradedHealth(t *testing.T) {
	srv := New(Dependencies{
		Reliability: fakeReliability{
			health: adminkernel.ReliabilityHealth{
				Status:                        adminkernel.ReliabilityStatusDegraded,
				Consistency:                   consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false),
				AllocationLifecycleRetries:    2,
				DueAllocationLifecycleRetries: 1,
				ReconcileUnhealthyComponents:  1,
				StorageBindingHealth: adminkernel.StorageBindingHealth{
					FailedBindings:         5,
					ReleasingBindings:      6,
					StuckReleasingBindings: 7,
					InconsistentClaims:     8,
					InvalidBindings:        9,
				},
				NodeVolumeHealth: adminkernel.NodeVolumeHealth{
					UnhealthyNodes:                1,
					PublishedVolumes:              2,
					LastReconcileStaleAllocations: 3,
					LastReconcileInvalidVolumes:   4,
					Error:                         "volumed reconcile failed",
				},
				Signals: []adminkernel.ReliabilitySignal{{
					Code:    adminkernel.ReliabilitySignalAllocationLifecycleRetries,
					Message: "2 allocation lifecycle retry item(s), 1 due",
				}, {
					Code:    adminkernel.ReliabilitySignalStorageBindings,
					Message: "1 failed storage binding(s), 0 stuck releasing",
				}, {
					Code:    adminkernel.ReliabilitySignalNodeVolumeManagers,
					Message: "1 node(s) report unhealthy volume manager",
				}},
			},
		},
	})

	resp, err := srv.GetAdminReliabilityHealth(context.Background(), &adminv1.GetAdminReliabilityHealthRequest{})
	if err != nil {
		t.Fatalf("GetAdminReliabilityHealth() error = %v", err)
	}
	got := resp.GetHealth()
	if got.GetStatus() != adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_DEGRADED {
		t.Fatalf("status = %s", got.GetStatus())
	}
	if got.GetAllocationLifecycleRetries() != 2 || got.GetDueAllocationLifecycleRetries() != 1 || got.GetReconcileUnhealthyComponents() != 1 {
		t.Fatalf("health counts = %+v", got)
	}
	if len(got.GetSignals()) != 3 ||
		got.GetSignals()[0].GetCode() != adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_ALLOCATION_LIFECYCLE_RETRIES ||
		got.GetSignals()[1].GetCode() != adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_STORAGE_BINDINGS ||
		got.GetSignals()[2].GetCode() != adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_NODE_VOLUME_MANAGERS {
		t.Fatalf("signals = %+v", got.GetSignals())
	}
	if got.GetNodeVolumeHealth().GetUnhealthyNodes() != 1 ||
		got.GetNodeVolumeHealth().GetPublishedVolumes() != 2 ||
		got.GetNodeVolumeHealth().GetLastReconcileStaleAllocations() != 3 ||
		got.GetNodeVolumeHealth().GetLastReconcileInvalidVolumes() != 4 ||
		got.GetNodeVolumeHealth().GetError() != "volumed reconcile failed" {
		t.Fatalf("node volume health = %+v", got.GetNodeVolumeHealth())
	}
	if got.GetStorageBindingHealth().GetFailedBindings() != 5 ||
		got.GetStorageBindingHealth().GetReleasingBindings() != 6 ||
		got.GetStorageBindingHealth().GetStuckReleasingBindings() != 7 ||
		got.GetStorageBindingHealth().GetInconsistentClaims() != 8 ||
		got.GetStorageBindingHealth().GetInvalidBindings() != 9 {
		t.Fatalf("storage binding health = %+v", got.GetStorageBindingHealth())
	}
}

func TestAdminReliabilityUnavailable(t *testing.T) {
	srv := New(Dependencies{})
	_, err := srv.CheckConsistency(context.Background(), &adminv1.CheckConsistencyRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("CheckConsistency() code = %s, want unavailable", status.Code(err))
	}
	_, err = srv.GetAdminReliabilityHealth(context.Background(), &adminv1.GetAdminReliabilityHealthRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("GetAdminReliabilityHealth() code = %s, want unavailable", status.Code(err))
	}
}

type fakeReliability struct {
	snapshot consistencykernel.Snapshot
	health   adminkernel.ReliabilityHealth
	err      error
}

func (f fakeReliability) ConsistencySnapshot(context.Context, time.Time) (consistencykernel.Snapshot, error) {
	if f.err != nil {
		return consistencykernel.Snapshot{}, f.err
	}
	return f.snapshot, nil
}

func (f fakeReliability) Health(context.Context, time.Time) (adminkernel.ReliabilityHealth, error) {
	if f.err != nil {
		return adminkernel.ReliabilityHealth{}, f.err
	}
	return f.health, nil
}

func TestAdminReliabilityPropagatesStoreError(t *testing.T) {
	srv := New(Dependencies{Reliability: fakeReliability{err: errors.New("database unavailable")}})
	_, err := srv.CheckConsistency(context.Background(), &adminv1.CheckConsistencyRequest{})
	if err == nil || err.Error() != "database unavailable" {
		t.Fatalf("CheckConsistency() error = %v", err)
	}
}
