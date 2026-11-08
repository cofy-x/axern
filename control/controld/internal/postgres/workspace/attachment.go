package pgworkspace

import (
	"context"
	"errors"
	"strings"
	"time"

	workspacekernel "github.com/cofy-x/axern/control/controld/internal/kernel/workspace"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	nodelifecyclev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/node/lifecycle/v1"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LockForAdmission follows the Namespace and Node locks held by resource
// admission. The preliminary lookup used for placement grants no authority.
func LockForAdmission(ctx context.Context, tx pgx.Tx, id, namespace, nodeID string, storage *nodev1.WorkspaceStorage) (*workspacekernel.Record, error) {
	r, err := scan(tx.QueryRow(ctx, selectSQL+` WHERE w.workspace_id=$1 FOR UPDATE OF w`, strings.TrimSpace(id)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "Workspace not found")
	}
	if err != nil {
		return nil, err
	}
	w := r.Workspace
	if w.GetNamespace() != namespace {
		return nil, status.Error(codes.NotFound, "Workspace not found")
	}
	if w.GetNodeID() != nodeID || w.GetStatus() != workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY || w.GetAttachedAllocationID() != "" || !storage.GetReady() || storage.GetStorageIdentity() != r.StorageRootIdentity {
		return nil, status.Error(codes.FailedPrecondition, "Workspace is unavailable or has an active or unresolved writer")
	}
	if w.GetGeneration() == int64(^uint64(0)>>1) {
		return nil, status.Error(codes.ResourceExhausted, "Workspace generation exhausted")
	}
	w.Generation++
	return r, nil
}

func AttachWriter(ctx context.Context, tx pgx.Tx, record *workspacekernel.Record, allocationID string, now time.Time) error {
	tag, err := tx.Exec(ctx, `UPDATE workspaces SET generation=$2,attached_allocation_id=$3,updated_at=$4 WHERE workspace_id=$1 AND attached_allocation_id IS NULL AND generation=$2-1 AND status='WORKSPACE_STATUS_READY'`, record.Workspace.GetID(), record.Workspace.GetGeneration(), allocationID, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return status.Error(codes.FailedPrecondition, "Workspace writer changed during admission")
	}
	return nil
}

func ReleaseWriter(ctx context.Context, tx pgx.Tx, binding *nodelifecyclev1.WorkspaceBinding, allocationID string, receipt *nodelifecyclev1.WorkspaceCleanupReceipt, now time.Time) error {
	if binding == nil {
		if receipt != nil {
			return status.Error(codes.FailedPrecondition, "unexpected Workspace cleanup receipt")
		}
		return nil
	}
	if receipt == nil || receipt.GetWorkspaceID() != binding.GetSpec().GetWorkspaceID() || receipt.GetAllocationID() != allocationID || receipt.GetGeneration() != binding.GetGeneration() || receipt.GetStorageIdentity() != binding.GetStorageIdentity() {
		return status.Error(codes.FailedPrecondition, "matching Workspace cleanup receipt is required before releasing its writer")
	}
	var generation int64
	var attached, identity string
	if err := tx.QueryRow(ctx, `SELECT generation,COALESCE(attached_allocation_id,''),storage_identity FROM workspaces WHERE workspace_id=$1 FOR UPDATE`, binding.GetSpec().GetWorkspaceID()).Scan(&generation, &attached, &identity); err != nil {
		return err
	}
	if generation != binding.GetGeneration() || attached != allocationID || identity != binding.GetStorageIdentity() {
		return status.Error(codes.FailedPrecondition, "Workspace cleanup does not own the current writer")
	}
	_, err := tx.Exec(ctx, `UPDATE workspaces SET attached_allocation_id=NULL,updated_at=$2 WHERE workspace_id=$1`, binding.GetSpec().GetWorkspaceID(), now)
	return err
}
