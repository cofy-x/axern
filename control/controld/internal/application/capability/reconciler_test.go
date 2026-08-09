package capability

import (
	"context"
	"errors"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeQueue struct {
	items      []allocationkernel.CapabilityReconcileItem
	completed  []string
	retried    []string
	conditions map[string][]*capabilityv1.CapabilityCondition
	admissions map[string]*allocationkernel.CapabilityAdmission
}

func (q *fakeQueue) Claim(context.Context, string, int, time.Time, time.Duration) ([]allocationkernel.CapabilityReconcileItem, error) {
	return append([]allocationkernel.CapabilityReconcileItem(nil), q.items...), nil
}
func (q *fakeQueue) Complete(_ context.Context, allocationID, _ string) error {
	q.completed = append(q.completed, allocationID)
	return nil
}
func (q *fakeQueue) Retry(_ context.Context, item allocationkernel.CapabilityReconcileItem, _ string, _ time.Time, _ error) error {
	q.retried = append(q.retried, item.AllocationID)
	return nil
}
func (q *fakeQueue) RecordAdmission(_ context.Context, item allocationkernel.CapabilityReconcileItem, _ string, admission *allocationkernel.CapabilityAdmission, _ time.Time) error {
	if q.conditions == nil {
		q.conditions = make(map[string][]*capabilityv1.CapabilityCondition)
	}
	q.conditions[item.AllocationID] = admission.Conditions
	if q.admissions == nil {
		q.admissions = make(map[string]*allocationkernel.CapabilityAdmission)
	}
	q.admissions[item.AllocationID] = admission
	return nil
}

type fakeLifecycle struct {
	statusResponse *privatenodev1.GetAllocationStatusResponse
	statusErr      error
	deleteErr      error
	deleteCalls    int
}

func (f *fakeLifecycle) GetAllocationStatus(context.Context, string, *privatenodev1.GetAllocationStatusRequest) (*privatenodev1.GetAllocationStatusResponse, error) {
	return f.statusResponse, f.statusErr
}
func (f *fakeLifecycle) DeleteAllocation(context.Context, string, *privatenodev1.DeleteAllocationRequest) (*privatenodev1.DeleteAllocationResponse, error) {
	f.deleteCalls++
	return &privatenodev1.DeleteAllocationResponse{}, f.deleteErr
}

func TestReconcilerCompletesHealthyVerification(t *testing.T) {
	queue := &fakeQueue{items: []allocationkernel.CapabilityReconcileItem{{AllocationID: "alloc-1"}}}
	client := &fakeLifecycle{statusResponse: &privatenodev1.GetAllocationStatusResponse{
		AdmittedCapabilityDependencies: []*capabilityv1.CapabilityDependency{{
			SelectedEvidence: &capabilityv1.CapabilityEvidence{EvidenceID: "reconciled-evidence"},
		}},
		CapabilityVerification: []*capabilityv1.CapabilityCondition{{
			State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
		}},
	}}
	if err := newReconciler(queue, client, "test-owner").Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.completed) != 1 || queue.completed[0] != "alloc-1" || len(queue.retried) != 0 {
		t.Fatalf("completed=%v retried=%v, want alloc-1 completed", queue.completed, queue.retried)
	}
	if len(queue.conditions["alloc-1"]) != 1 {
		t.Fatalf("recorded conditions = %v, want one", queue.conditions)
	}
	if len(queue.admissions["alloc-1"].Dependencies) != 1 || queue.admissions["alloc-1"].Dependencies[0].GetSelectedEvidence().GetEvidenceID() != "reconciled-evidence" {
		t.Fatalf("recorded admission = %#v", queue.admissions["alloc-1"])
	}
}

func TestReconcilerRetainsFailedAllocationUntilNotFound(t *testing.T) {
	queue := &fakeQueue{items: []allocationkernel.CapabilityReconcileItem{{AllocationID: "alloc-1", NodeID: "node-1", NodeTarget: "node:25001", Attempt: 2}}}
	client := &fakeLifecycle{statusResponse: &privatenodev1.GetAllocationStatusResponse{CapabilityVerification: []*capabilityv1.CapabilityCondition{{
		State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED,
	}}}}
	reconciler := newReconciler(queue, client, "test-owner")
	if err := reconciler.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if client.deleteCalls != 1 || len(queue.retried) != 1 || len(queue.completed) != 0 {
		t.Fatalf("delete=%d retried=%v completed=%v, want delete and durable retry", client.deleteCalls, queue.retried, queue.completed)
	}

	queue.retried = nil
	client.statusResponse = nil
	client.statusErr = status.Error(codes.NotFound, "gone")
	if err := reconciler.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.completed) != 1 || queue.completed[0] != "alloc-1" || len(queue.retried) != 0 {
		t.Fatalf("completed=%v retried=%v, want completion only after not found", queue.completed, queue.retried)
	}
}

func TestReconcilerRetriesInconclusiveStatus(t *testing.T) {
	queue := &fakeQueue{items: []allocationkernel.CapabilityReconcileItem{{AllocationID: "alloc-1"}}}
	client := &fakeLifecycle{statusErr: errors.New("node unavailable")}
	if err := newReconciler(queue, client, "test-owner").Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(queue.retried) != 1 || len(queue.completed) != 0 {
		t.Fatalf("retried=%v completed=%v, want retry", queue.retried, queue.completed)
	}
}
