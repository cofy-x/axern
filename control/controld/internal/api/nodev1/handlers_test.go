package nodev1

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	leasekernel "github.com/cofy-x/axern/control/controld/internal/kernel/lease"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func TestValidateNodeMemoryBudgetRequiresCanonicalFreshSummary(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	summary := controldtest.ReadySummary(now)
	summary.Capacity.MemoryBytes = 16 << 30
	summary.Allocatable.MemoryBytes = 7 << 30
	summary.MemoryBudget = &controlnodev1.NodeMemoryBudget{
		PhysicalCapacityBytes: 16 << 30, SourceAllocatableBytes: 8 << 30, SystemReserveBytes: 1 << 30,
		EffectiveAllocatableBytes: 7 << 30, CapacityIdentity: "boot:mount:root:sandbox",
		Mode:      controlnodev1.NodeMemoryBudgetMode_NODE_MEMORY_BUDGET_MODE_CGROUP_V2,
		SampledAt: timestamppb.New(now),
	}
	if err := validateNodeMemoryBudget(summary, now); err != nil {
		t.Fatalf("validateNodeMemoryBudget() error = %v", err)
	}
	summary.Allocatable.MemoryBytes++
	if err := validateNodeMemoryBudget(summary, now); err == nil {
		t.Fatal("validateNodeMemoryBudget() accepted mismatched allocatable")
	}
	summary.Allocatable.MemoryBytes--
	summary.MemoryBudget.SampledAt = timestamppb.New(now.Add(-time.Minute))
	if err := validateNodeMemoryBudget(summary, now); err == nil {
		t.Fatal("validateNodeMemoryBudget() accepted stale sample")
	}
}

func TestBatchReportAllocationLifecycleAuthenticatesAndForwardsBatch(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	nodeStore := controldtest.NewMemoryNodeStore()
	if _, err := nodeStore.Register(context.Background(), nodekernel.RegisterParams{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Now:           now,
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	allocations := &fakeAllocationControl{}
	server := New(Dependencies{
		Now:         func() time.Time { return now },
		NodeStore:   nodeStore,
		Allocations: allocations,
	})
	observations := []*controlnodev1.AllocationLifecycleObservation{
		{AllocationID: "alloc-1", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STARTING, ObservedAt: timestamppb.New(now)},
		{AllocationID: "alloc-2", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, ObservedAt: timestamppb.New(now)},
	}

	if _, err := server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations:  observations,
	}); err != nil {
		t.Fatalf("BatchReportAllocationLifecycle() error = %v", err)
	}
	if allocations.calls != 1 || allocations.nodeID != "node-a" || len(allocations.observations) != 2 {
		t.Fatalf("allocation control call = calls:%d node:%q observations:%d", allocations.calls, allocations.nodeID, len(allocations.observations))
	}

	_, err := server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationLifecycleObservation{
			{AllocationID: "alloc-1", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, ObservedAt: timestamppb.New(now)},
			{AllocationID: "alloc-1", State: commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE, ObservedAt: timestamppb.New(now)},
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("duplicate allocation error = %v, want InvalidArgument", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after invalid batch = %d, want 1", allocations.calls)
	}

	_, err = server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationLifecycleObservation{{
			AllocationID: "alloc-1",
			State:        commonv1.AllocationLifecycleState(999),
		}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown allocation lifecycle error = %v, want InvalidArgument", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after unknown lifecycle state = %d, want 1", allocations.calls)
	}

	_, err = server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationLifecycleObservation{{
			AllocationID: "alloc-1",
			State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING,
		}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("control-plane allocation lifecycle state error = %v, want InvalidArgument", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after control-plane state = %d, want 1", allocations.calls)
	}

	_, err = server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationLifecycleObservation{{
			AllocationID:   "alloc-1",
			State:          commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
			DiagnosticCode: commonv1.WorkloadDiagnosticCode(999),
		}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("unknown diagnostic code error = %v, want InvalidArgument", err)
	}
	if allocations.calls != 1 {
		t.Fatalf("allocation control calls after unknown diagnostic code = %d, want 1", allocations.calls)
	}

	_, err = server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationLifecycleObservation{{
			AllocationID: "alloc-1",
			State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
		}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing observed_at error = %v, want InvalidArgument", err)
	}

	_, err = server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
		NodeID:        "node-a",
		NodeAuthToken: "token-a",
		Observations: []*controlnodev1.AllocationLifecycleObservation{{
			AllocationID: "alloc-1",
			State:        commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_ACTIVE,
			ObservedAt:   timestamppb.New(now),
			ExitCode:     func() *int32 { value := int32(0); return &value }(),
		}},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("active terminal facts error = %v, want InvalidArgument", err)
	}

	_, err = server.BatchReportAllocationLifecycle(context.Background(), &controlnodev1.BatchReportAllocationLifecycleRequest{
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
}

func TestBatchReportAllocationCapabilityConditionsIsAuthenticatedAndConditionOnly(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	nodeStore := controldtest.NewMemoryNodeStore()
	if _, err := nodeStore.Register(context.Background(), nodekernel.RegisterParams{NodeID: "node-a", NodeAuthToken: "token-a", Now: now}); err != nil {
		t.Fatal(err)
	}
	allocations := &fakeAllocationControl{}
	server := New(Dependencies{Now: func() time.Time { return now }, NodeStore: nodeStore, Allocations: allocations})
	report := validCapabilityConditionReport(now)
	if _, err := server.BatchReportAllocationCapabilityConditions(context.Background(), &controlnodev1.BatchReportAllocationCapabilityConditionsRequest{
		NodeID: "node-a", NodeAuthToken: "token-a", Reports: []*controlnodev1.AllocationCapabilityConditionReport{report},
	}); err != nil {
		t.Fatal(err)
	}
	if allocations.conditionCalls != 1 || allocations.conditionNodeID != "node-a" || len(allocations.conditionReports) != 1 {
		t.Fatalf("condition forwarding = calls:%d node:%q reports:%d", allocations.conditionCalls, allocations.conditionNodeID, len(allocations.conditionReports))
	}
	if allocations.calls != 0 {
		t.Fatalf("condition reporting invoked lifecycle observation path %d time(s)", allocations.calls)
	}

	duplicate := []*controlnodev1.AllocationCapabilityConditionReport{report, report}
	_, err := server.BatchReportAllocationCapabilityConditions(context.Background(), &controlnodev1.BatchReportAllocationCapabilityConditionsRequest{
		NodeID: "node-a", NodeAuthToken: "token-a", Reports: duplicate,
	})
	if grpcstatus.Code(err) != codes.InvalidArgument || allocations.conditionCalls != 1 {
		t.Fatalf("duplicate condition report error=%v calls=%d", err, allocations.conditionCalls)
	}
	_, err = server.BatchReportAllocationCapabilityConditions(context.Background(), &controlnodev1.BatchReportAllocationCapabilityConditionsRequest{
		NodeID: "node-a", NodeAuthToken: "wrong", Reports: []*controlnodev1.AllocationCapabilityConditionReport{report},
	})
	if grpcstatus.Code(err) != codes.PermissionDenied || allocations.conditionCalls != 1 {
		t.Fatalf("unauthenticated condition report error=%v calls=%d", err, allocations.conditionCalls)
	}
}

func validCapabilityConditionReport(now time.Time) *controlnodev1.AllocationCapabilityConditionReport {
	key := capabilitycontract.ExtensionKey("example.com/accelerator", "v1")
	return &controlnodev1.AllocationCapabilityConditionReport{
		AllocationID: "allocation-a",
		ConditionSet: &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{{
			Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		}}},
	}
}

func TestReportTunnelSessionStatusRequiresNodeAuth(t *testing.T) {
	now := time.Now().UTC()
	nodeStore := controldtest.NewMemoryNodeStore()
	if _, err := nodeStore.Register(context.Background(), nodekernel.RegisterParams{
		NodeID:        "node-a",
		NodeTarget:    "127.0.0.1:25000",
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
	calls            int
	nodeID           string
	observations     []*controlnodev1.AllocationLifecycleObservation
	conditionCalls   int
	conditionNodeID  string
	conditionReports []*controlnodev1.AllocationCapabilityConditionReport
}

func (f *fakeAllocationControl) BatchReportAllocationLifecycle(_ context.Context, nodeID string, observations []*controlnodev1.AllocationLifecycleObservation, _ time.Time) ([]string, error) {
	f.calls++
	f.nodeID = nodeID
	f.observations = append([]*controlnodev1.AllocationLifecycleObservation(nil), observations...)
	return nil, nil
}

func (f *fakeAllocationControl) BatchReportAllocationCapabilityConditions(_ context.Context, nodeID string, reports []*controlnodev1.AllocationCapabilityConditionReport, _ time.Time) error {
	f.conditionCalls++
	f.conditionNodeID = nodeID
	f.conditionReports = append([]*controlnodev1.AllocationCapabilityConditionReport(nil), reports...)
	return nil
}

func (f *fakeAllocationControl) ReconcileNodeInventory(context.Context, allocationkernel.NodeInventorySnapshot, time.Time) error {
	return nil
}

func (f *fakeAllocationControl) WatchExecutionLeases(context.Context, string, int64, time.Time) ([]*leasekernel.Record, int64, error) {
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
