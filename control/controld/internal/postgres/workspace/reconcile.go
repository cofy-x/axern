package pgworkspace

import (
	"context"
	"errors"
	"fmt"
	"time"

	workspacekernel "github.com/cofy-x/axern/control/controld/internal/kernel/workspace"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ClaimWorkspaceReconcile(ctx context.Context, owner string, now time.Time) (*workspacekernel.ReconcileItem, error) {
	var item *workspacekernel.ReconcileItem
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		r, err := scan(tx.QueryRow(ctx, selectSQL+` WHERE w.status<>'WORKSPACE_STATUS_DELETED' AND w.next_reconcile_at<=$1 AND (w.claim_owner='' OR w.claim_expires_at<=$1) ORDER BY w.next_reconcile_at,w.workspace_id LIMIT 1 FOR UPDATE OF w SKIP LOCKED`, now))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workspaces SET claim_owner=$2,claim_expires_at=$3 WHERE workspace_id=$1`, r.Workspace.GetID(), owner, now.Add(2*time.Minute)); err != nil {
			return err
		}
		item = &workspacekernel.ReconcileItem{Record: r, ClaimOwner: owner}
		return nil
	})
	return item, err
}

func (s *Store) CompleteWorkspaceReconcile(ctx context.Context, item *workspacekernel.ReconcileItem, identity string, usedBytes, usedInodes int64, operationErr error, now time.Time) error {
	return s.withTx(ctx, func(tx pgx.Tx) error {
		var state, claimOwner, currentIdentity string
		var expires *time.Time
		if err := tx.QueryRow(ctx, `SELECT status,claim_owner,storage_identity,claim_expires_at FROM workspaces WHERE workspace_id=$1 FOR UPDATE`, item.Record.Workspace.GetID()).Scan(&state, &claimOwner, &currentIdentity, &expires); err != nil {
			return err
		}
		if claimOwner != item.ClaimOwner || expires == nil || !expires.After(now) {
			return fmt.Errorf("Workspace reconciliation claim changed")
		}
		if identity != "" {
			if currentIdentity != "" && currentIdentity != identity {
				return fmt.Errorf("Workspace storage identity changed")
			}
			currentIdentity = identity
		}
		status := workspacev1.WorkspaceStatus(workspacev1.WorkspaceStatus_value[state])
		message := ""
		if operationErr != nil {
			message = "managed storage operation failed"
			if status == workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY {
				status = workspacev1.WorkspaceStatus_WORKSPACE_STATUS_UNAVAILABLE
			}
		} else {
			switch item.Record.Workspace.GetStatus() {
			case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING:
				if identity == "" || currentIdentity != "" && currentIdentity != identity {
					return fmt.Errorf("Workspace provisioning identity changed")
				}
				currentIdentity = identity
				if status == workspacev1.WorkspaceStatus_WORKSPACE_STATUS_PROVISIONING {
					status = workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY
				}
			case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING:
				if status != workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING {
					return fmt.Errorf("Workspace deletion intent changed")
				}
				status = workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETED
			case workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY, workspacev1.WorkspaceStatus_WORKSPACE_STATUS_UNAVAILABLE:
				if status != workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETING {
					status = workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY
				}
			}
		}
		if usedBytes < 0 || usedInodes < 0 {
			return fmt.Errorf("negative Workspace usage observation")
		}
		next := now.Add(5 * time.Second)
		if status == workspacev1.WorkspaceStatus_WORKSPACE_STATUS_READY {
			next = now.Add(10 * time.Second)
		}
		_, err := tx.Exec(ctx, `UPDATE workspaces SET status=$2,storage_identity=$3,used_bytes=$4,used_inodes=$5,message=$6,updated_at=$7,next_reconcile_at=$8,claim_owner='',claim_expires_at=NULL WHERE workspace_id=$1`, item.Record.Workspace.GetID(), status.String(), currentIdentity, usedBytes, usedInodes, message, now, next)
		return err
	})
}
