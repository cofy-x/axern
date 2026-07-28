package nodev1

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestReportNodeRequiresRuntimeSlotContract(t *testing.T) {
	server := New(Dependencies{})

	_, err := server.ReportNode(context.Background(), &controlnodev1.ReportNodeRequest{
		NodeID:  "node-a",
		Summary: &controlnodev1.NodeSummary{Pools: &controlnodev1.PoolsSummary{}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("ReportNode() error = %v, want InvalidArgument", err)
	}
	if got := grpcstatus.Convert(err).Message(); got != "summary.pools.runtime_slots is required" {
		t.Fatalf("ReportNode() error message = %q", got)
	}
}

func TestBatchReportAllocationStatusAuthenticatesAndForwardsBatch(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	nodeStore := controldtest.NewMemoryNodeStore()
	if _, err := nodeStore.Register(context.Background(), nodekernel.RegisterParams{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Now:           now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	allocations := &fakeAllocationControl{reconcileServiceIDs: []string{"svc-2", "svc-1"}}
	notifications := 0
	var notifiedServiceIDs []string
	server := New(Dependencies{
		Now:         func() time.Time { return now },
		NodeStore:   nodeStore,
		Allocations: allocations,
		NotifyServiceReconcile: func(serviceIDs ...string) {
			notifications++
			notifiedServiceIDs = append(notifiedServiceIDs, serviceIDs...)
		},
	})
	observations := []*controlnodev1.AllocationStatusObservation{
		{AllocationID: "alloc-1", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING},
		{AllocationID: "alloc-2", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
	}

	if _, err := server.BatchReportAllocationStatus(context.Background(), &controlnodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations:  observations,
	}); err != nil {
		t.Fatalf("BatchReportAllocationStatus() error = %v", err)
	}
	if allocations.calls != 1 || allocations.nodeID != "node-a" || len(allocations.observations) != 2 {
		t.Fatalf("allocation control call = calls:%d node:%q observations:%d", allocations.calls, allocations.nodeID, len(allocations.observations))
	}
	if notifications != 1 {
		t.Fatalf("service reconcile notifications = %d, want 1", notifications)
	}
	if len(notifiedServiceIDs) != 2 || notifiedServiceIDs[0] != "svc-2" || notifiedServiceIDs[1] != "svc-1" {
		t.Fatalf("notified service IDs = %#v, want [svc-2 svc-1]", notifiedServiceIDs)
	}

	_, err := server.BatchReportAllocationStatus(context.Background(), &controlnodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationStatusObservation{
			{AllocationID: "alloc-1", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
			{AllocationID: "alloc-1", Attempt: 1, Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING},
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate allocation error = %v, want InvalidArgument", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after invalid batch = %d, want 1", allocations.calls)
	}
	if notifications != 1 {
		t.Fatalf("service reconcile notifications after invalid batch = %d, want 1", notifications)
	}

	_, err = server.BatchReportAllocationStatus(context.Background(), &controlnodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationStatusObservation{{
			AllocationID: "alloc-1",
			Attempt:      1,
			Status:       commonv1.AllocationStatus(999),
		}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown allocation status error = %v, want InvalidArgument", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after unknown status = %d, want 1", allocations.calls)
	}

	_, err = server.BatchReportAllocationStatus(context.Background(), &controlnodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "wrong-token",
		Observations:  observations,
	})
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("invalid auth error = %v, want PermissionDenied", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after invalid auth = %d, want 1", allocations.calls)
	}
	if notifications != 1 {
		t.Fatalf("service reconcile notifications after invalid auth = %d, want 1", notifications)
	}

	allocations.reconcileServiceIDs = nil
	if _, err := server.BatchReportAllocationStatus(context.Background(), &controlnodev1.BatchReportAllocationStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations:  observations,
	}); err != nil {
		t.Fatalf("run-only BatchReportAllocationStatus() error = %v", err)
	}
	if notifications != 1 {
		t.Fatalf("service reconcile notifications after run-only batch = %d, want 1", notifications)
	}
}

func TestReportTunnelSessionStatusRequiresNodeAuth(t *testing.T) {
	now := time.Now().UTC()
	nodeStore := controldtest.NewMemoryNodeStore()
	if _, err := nodeStore.Register(context.Background(), nodekernel.RegisterParams{
		NodeID:        "node-a",
		NodeTarget:    "127.0.0.1:25000",
		Runtimes:      []string{"runsc"},
		NodeAuthToken: "token-a",
		Now:           now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	tunnels := &fakeTunnelControl{}
	server := New(Dependencies{
		Now:       func() time.Time { return now },
		NodeStore: nodeStore,
		Tunnels:   tunnels,
	})

	_, err := server.ReportTunnelSessionStatus(context.Background(), &controlnodev1.ReportTunnelSessionStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "wrong-token",
		SessionID:     "tun-1",
		Status:        tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
	})
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("ReportTunnelSessionStatus error = %v, want PermissionDenied", err)
	}
	if tunnels.reportCalled {
		t.Fatal("tunnel status reporter was called despite invalid node auth")
	}

	if _, err := server.ReportTunnelSessionStatus(context.Background(), &controlnodev1.ReportTunnelSessionStatusRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		SessionID:     "tun-1",
		Status:        tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
	}); err != nil {
		t.Fatalf("ReportTunnelSessionStatus with valid node auth: %v", err)
	}
	if !tunnels.reportCalled || tunnels.nodeID != "node-a" || tunnels.sessionID != "tun-1" {
		t.Fatalf("report call = called:%t node:%q session:%q", tunnels.reportCalled, tunnels.nodeID, tunnels.sessionID)
	}
}

type fakeTunnelControl struct {
	reportCalled bool
	nodeID       string
	sessionID    string
}

type fakeAllocationControl struct {
	calls               int
	nodeID              string
	observations        []*controlnodev1.AllocationStatusObservation
	reconcileServiceIDs []string
}

func (f *fakeAllocationControl) BatchReportAllocationStatus(_ context.Context, nodeID string, observations []*controlnodev1.AllocationStatusObservation, _ time.Time) ([]string, error) {
	f.calls++
	f.nodeID = nodeID
	f.observations = append([]*controlnodev1.AllocationStatusObservation(nil), observations...)
	return f.reconcileServiceIDs, nil
}

func (f *fakeAllocationControl) ReconcileNodeInventory(context.Context, allocationkernel.NodeInventorySnapshot, time.Time) error {
	return nil
}

func (f *fakeAllocationControl) WatchExecutionLeases(context.Context, string, int64, time.Time) ([]*commonv1.ExecutionLease, int64, error) {
	return nil, 0, nil
}

func (f *fakeTunnelControl) WatchNode(ctx context.Context, nodeID string, afterRevision int64, now time.Time) ([]*controlnodev1.NodeTunnelSession, int64, error) {
	_ = ctx
	_ = nodeID
	_ = afterRevision
	_ = now
	return nil, 0, nil
}

func (f *fakeTunnelControl) ReportStatus(ctx context.Context, nodeID, sessionID string, status tunnelv1.TunnelSessionStatus, reason, boundAddr string, now time.Time) (*tunnelv1.TunnelSession, error) {
	_ = ctx
	_ = status
	_ = reason
	_ = boundAddr
	_ = now
	f.reportCalled = true
	f.nodeID = nodeID
	f.sessionID = sessionID
	return &tunnelv1.TunnelSession{SessionID: sessionID, NodeID: nodeID, Status: status}, nil
}
