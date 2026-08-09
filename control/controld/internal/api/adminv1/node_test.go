package adminv1

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestListAdminNodesMapsLifecycleAndHealth(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	nodes := &fakeNodeAdmin{records: []*nodekernel.Record{{
		NodeID: "node-a", Lifecycle: nodekernel.LifecycleActive,
		RegisteredAt: now.Add(-time.Hour), UpdatedAt: now.Add(-5 * time.Second),
	}}}
	srv := New(Dependencies{Now: func() time.Time { return now }, Nodes: nodes, NodeHeartbeatWindow: 15 * time.Second, NodeSummaryWindow: 15 * time.Second})

	resp, err := srv.ListAdminNodes(context.Background(), &adminv1.ListAdminNodesRequest{LifecycleStatus: adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_ACTIVE})
	if err != nil {
		t.Fatalf("ListAdminNodes() error = %v", err)
	}
	if nodes.filter.Lifecycle != nodekernel.LifecycleActive || len(resp.GetNodes()) != 1 || !resp.GetNodes()[0].GetHeartbeatFresh() {
		t.Fatalf("filter = %+v, response = %+v", nodes.filter, resp)
	}
}

func TestRetireAdminNodeForwardsNormalizedRequest(t *testing.T) {
	now := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	nodes := &fakeNodeAdmin{record: &nodekernel.Record{NodeID: "node-a", Lifecycle: nodekernel.LifecycleRetired, RetiredAt: now, RetiredReason: "host removed"}}
	srv := New(Dependencies{Now: func() time.Time { return now }, Nodes: nodes, NodeHeartbeatWindow: time.Minute, NodeSummaryWindow: time.Minute})

	resp, err := srv.RetireAdminNode(context.Background(), &adminv1.RetireAdminNodeRequest{NodeID: " node-a ", OperatorReason: " host removed "})
	if err != nil {
		t.Fatalf("RetireAdminNode() error = %v", err)
	}
	if nodes.nodeID != "node-a" || nodes.reason != "host removed" || resp.GetNode().GetLifecycleStatus() != adminv1.AdminNodeLifecycleStatus_ADMIN_NODE_LIFECYCLE_STATUS_RETIRED {
		t.Fatalf("nodeID = %q, reason = %q, response = %+v", nodes.nodeID, nodes.reason, resp)
	}
}

func TestGetAllocationCapabilityDiagnosticsPreservesAttemptFence(t *testing.T) {
	admittedAt := time.Date(2026, 7, 26, 12, 1, 0, 0, time.UTC)
	diagnostics := &fakeCapabilityDiagnostics{allocation: &adminkernel.AllocationCapabilityDiagnostics{
		AllocationID:              "allocation-a",
		NodeID:                    "node-a",
		Attempt:                   7,
		CreateAdmissionRecorded:   true,
		CreateDependencySetDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		CreateAdmittedAt:          &admittedAt,
		ConditionSet:              &capabilityv1.CapabilityConditionSet{Revision: 3},
	}}
	srv := New(Dependencies{CapabilityDiagnostics: diagnostics})

	resp, err := srv.GetAllocationCapabilityDiagnostics(context.Background(), &adminv1.GetAllocationCapabilityDiagnosticsRequest{AllocationID: " allocation-a "})
	if err != nil {
		t.Fatalf("GetAllocationCapabilityDiagnostics() error = %v", err)
	}
	if diagnostics.allocationID != "allocation-a" || resp.GetAllocationAttempt() != 7 || resp.GetConditionSet().GetRevision() != 3 ||
		!resp.GetCreateAdmissionRecorded() || resp.GetCreateDependencySetDigest() != diagnostics.allocation.CreateDependencySetDigest ||
		!resp.GetCreateAdmittedAt().AsTime().Equal(admittedAt) {
		t.Fatalf("allocationID = %q, response = %+v", diagnostics.allocationID, resp)
	}
}

func TestCapabilityDiagnosticListsRejectNegativeLimit(t *testing.T) {
	srv := New(Dependencies{CapabilityDiagnostics: &fakeCapabilityDiagnostics{}})
	tests := []struct {
		name string
		call func() error
	}{
		{
			name: "transitions",
			call: func() error {
				_, err := srv.ListNodeCapabilityTransitions(context.Background(), &adminv1.ListNodeCapabilityTransitionsRequest{Limit: -1})
				return err
			},
		},
		{
			name: "backlog",
			call: func() error {
				_, err := srv.ListCapabilityReconcileQueue(context.Background(), &adminv1.ListCapabilityReconcileQueueRequest{Limit: -1})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); grpcstatus.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v, want InvalidArgument", err)
			}
		})
	}
}

type fakeNodeAdmin struct {
	filter  adminkernel.NodeListFilter
	records []*nodekernel.Record
	record  *nodekernel.Record
	nodeID  string
	reason  string
}

func (f *fakeNodeAdmin) ListNodes(_ context.Context, filter adminkernel.NodeListFilter) ([]*nodekernel.Record, error) {
	f.filter = filter
	return f.records, nil
}

func (f *fakeNodeAdmin) RetireNode(_ context.Context, nodeID, reason string, _ time.Time) (*nodekernel.Record, error) {
	f.nodeID = nodeID
	f.reason = reason
	return f.record, nil
}

type fakeCapabilityDiagnostics struct {
	allocationID string
	allocation   *adminkernel.AllocationCapabilityDiagnostics
}

func (*fakeCapabilityDiagnostics) GetNodeCapabilitySnapshot(context.Context, string) (*capabilityv1.CapabilitySnapshot, error) {
	return nil, nil
}

func (*fakeCapabilityDiagnostics) ListNodeCapabilityTransitions(context.Context, string, int32) ([]adminkernel.CapabilityTransition, error) {
	return nil, nil
}

func (*fakeCapabilityDiagnostics) ListCapabilityReconcileQueue(context.Context, string, int32) ([]adminkernel.CapabilityReconcileItem, error) {
	return nil, nil
}

func (f *fakeCapabilityDiagnostics) GetAllocationCapabilityDiagnostics(_ context.Context, allocationID string) (*adminkernel.AllocationCapabilityDiagnostics, error) {
	f.allocationID = allocationID
	return f.allocation, nil
}
