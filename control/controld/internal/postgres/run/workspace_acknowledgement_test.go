package pgrun

import (
	"context"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	retentionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/retention"
	"github.com/cofy-x/axern/control/controld/internal/nodebridge"
	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	pgretention "github.com/cofy-x/axern/control/controld/internal/postgres/retention"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// A release receipt remains node-owned until the normal acknowledgement
// delivers the exact frozen authority. This fake exercises only that RPC.
type workspaceAcknowledgementClient struct {
	controldtest.FakeNodeLifecycleClient
	receipt *nodelifecyclev1.WorkspaceCleanupReceipt
	err     error
}

func (c *workspaceAcknowledgementClient) AcknowledgeAllocationRelease(_ context.Context, _ string, req *nodelifecyclev1.AcknowledgeAllocationReleaseRequest) (*nodelifecyclev1.AcknowledgeAllocationReleaseResponse, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.receipt == nil || !proto.Equal(req.GetWorkspaceCleanup(), c.receipt) || req.GetAllocationID() != c.receipt.GetAllocationID() {
		return nil, grpcstatus.Error(codes.FailedPrecondition, "release acknowledgement lost the exact Workspace receipt")
	}
	c.receipt = nil
	return &nodelifecyclev1.AcknowledgeAllocationReleaseResponse{}, nil
}

func TestWorkspaceReleaseAcknowledgementCannotBeClearedOrRetainedAway(t *testing.T) {
	f := newWorkspaceFixture(t)
	run, err := f.store.AdmitRun(f.ctx, f.params, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CancelRun(f.ctx, run.GetID(), f.now); err != nil {
		t.Fatal(err)
	}
	items, err := f.store.ClaimDueReconcileItems(f.ctx, "release-worker", 10, f.now, time.Minute)
	if err != nil || len(items) != 1 {
		t.Fatalf("release claim=%+v error=%v", items, err)
	}
	if err := f.store.CompleteAllocationReleaseAcknowledgement(f.ctx, run.GetAllocationID(), items[0].ClaimOwner, f.now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("acknowledgement before release=%v", err)
	}
	receipt := &nodelifecyclev1.WorkspaceCleanupReceipt{WorkspaceID: f.workspace.GetID(), AllocationID: run.GetAllocationID(), Generation: 1, StorageIdentity: "opaque-owned-fixture"}
	if err := f.store.CompleteAllocationRelease(f.ctx, run.GetAllocationID(), items[0].ClaimOwner, &allocationkernel.ReleaseResult{WorkspaceCleanup: receipt}, f.now); err != nil {
		t.Fatal(err)
	}
	admin := pgadmin.NewStore(f.db)
	retries, err := admin.ListAllocationLifecycleRetries(f.ctx, allocationkernel.LifecycleRetryFilter{}, f.now)
	if err != nil || len(retries) != 1 {
		t.Fatalf("release retries=%+v error=%v", retries, err)
	}
	if retries[0].LifecycleState != commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String() || retries[0].Clearable || retries[0].ClearBlockedReason != "Workspace release acknowledgement is pending" {
		t.Fatalf("pending acknowledgement clearance=%+v", retries[0])
	}
	if _, err := admin.ClearAllocationLifecycleRetry(f.ctx, allocationkernel.ClearLifecycleRetryRequest{AllocationID: run.GetAllocationID(), OperatorReason: "test pending receipt protection"}, f.now); grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("manual clear before acknowledgement=%v", err)
	}
	retention := pgretention.NewPGStore(f.db)
	config := retentionkernel.DefaultConfig()
	config.TerminalRunsTTL = time.Hour
	result, err := retention.Cleanup(f.ctx, config, f.now.Add(48*time.Hour))
	if err != nil || result.TerminalRunsDeleted != 0 {
		t.Fatalf("retention before acknowledgement=%+v error=%v", result, err)
	}
	items, err = f.store.ClaimDueReconcileItems(f.ctx, "ack-worker", 10, f.now, time.Minute)
	if err != nil || len(items) != 1 || items[0].Workspace == nil {
		t.Fatalf("acknowledgement claim lost frozen binding: %+v error=%v", items, err)
	}
	item := items[0]
	node := &workspaceAcknowledgementClient{receipt: receipt, err: grpcstatus.Error(codes.Unavailable, "transient node outage")}
	bridge := nodebridge.New(node, nodebridge.Config{})
	ack := func() error {
		return bridge.AcknowledgeAllocationRelease(f.ctx, item.NodeTarget, item.AllocationID, item.NodeID, item.Workspace)
	}
	if err := ack(); grpcstatus.Code(err) != codes.Unavailable || node.receipt == nil {
		t.Fatalf("transient acknowledgement lost receipt: error=%v receipt=%+v", err, node.receipt)
	}
	var queued int
	if err := f.db.Pool().QueryRow(f.ctx, `SELECT COUNT(*) FROM allocation_reconcile_queue WHERE allocation_id=$1`, run.GetAllocationID()).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("pending acknowledgement queue=%d error=%v", queued, err)
	}
	node.err = nil
	if err := ack(); err != nil || node.receipt != nil {
		t.Fatalf("exact acknowledgement failed: error=%v receipt=%+v", err, node.receipt)
	}
	if err := f.store.CompleteAllocationReleaseAcknowledgement(f.ctx, item.AllocationID, item.ClaimOwner, f.now); err != nil {
		t.Fatal(err)
	}
	result, err = retention.Cleanup(f.ctx, config, f.now.Add(48*time.Hour))
	if err != nil || result.TerminalRunsDeleted != 1 {
		t.Fatalf("retention after acknowledgement=%+v error=%v", result, err)
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil || w.GetAttachedAllocationID() != "" || w.GetGeneration() != 1 || w.GetCapacityBytes() != f.workspace.GetCapacityBytes() {
		t.Fatalf("acknowledgement changed retained Workspace: %+v error=%v", w, err)
	}
}
