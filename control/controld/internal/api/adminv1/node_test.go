package adminv1

import (
	"context"
	"testing"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func TestGetAllocationCapabilityDiagnostics(t *testing.T) {
	admittedAt := time.Date(2026, 7, 26, 12, 1, 0, 0, time.UTC)
	diagnostics := &fakeCapabilityDiagnostics{allocation: &adminkernel.AllocationCapabilityDiagnostics{
		AllocationID:            "allocation-a",
		NodeID:                  "node-a",
		ConditionSet:            &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(admittedAt)},
		LatestMemoryObservation: &nodev1.AllocationMemoryObservation{Revision: 9, CurrentBytes: 64 << 20},
	}}
	srv := New(Dependencies{CapabilityDiagnostics: diagnostics})

	resp, err := srv.GetAllocationCapabilityDiagnostics(context.Background(), &adminv1.GetAllocationCapabilityDiagnosticsRequest{AllocationID: " allocation-a "})
	if err != nil {
		t.Fatalf("GetAllocationCapabilityDiagnostics() error = %v", err)
	}
	if diagnostics.allocationID != "allocation-a" || !resp.GetConditionSet().GetObservedAt().AsTime().Equal(admittedAt) ||
		resp.GetLatestMemoryObservation().GetRevision() != 9 {
		t.Fatalf("allocationID = %q, response = %+v", diagnostics.allocationID, resp)
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

func (f *fakeCapabilityDiagnostics) GetAllocationCapabilityDiagnostics(_ context.Context, allocationID string) (*adminkernel.AllocationCapabilityDiagnostics, error) {
	f.allocationID = allocationID
	return f.allocation, nil
}
