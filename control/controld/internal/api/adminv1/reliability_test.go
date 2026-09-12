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
				Signals: []adminkernel.ReliabilitySignal{{
					Code:    adminkernel.ReliabilitySignalAllocationLifecycleRetries,
					Message: "2 allocation lifecycle retry item(s), 1 due",
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
	if len(got.GetSignals()) != 1 || got.GetSignals()[0].GetCode() != adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_ALLOCATION_LIFECYCLE_RETRIES {
		t.Fatalf("signals = %+v", got.GetSignals())
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
