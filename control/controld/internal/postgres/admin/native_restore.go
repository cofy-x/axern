package pgadmin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	"github.com/cofy-x/axern/internal/proto/nativebackup"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PlanNativeRestore reads only the authoritative detached inventory while the
// native maintenance operator keeps the original and target installations fenced.
func (s *Store) PlanNativeRestore(ctx context.Context, oldNode, newNode string) (nativebackup.Plan, error) {
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return nativebackup.Plan{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := nativeRestoreFence(ctx, tx, oldNode, newNode, ""); err != nil {
		return nativebackup.Plan{}, err
	}
	workspaces, err := nativeRestoreInventory(ctx, tx, oldNode)
	if err != nil {
		return nativebackup.Plan{}, err
	}
	plan := nativebackup.Plan{Format: nativebackup.Format, OldNode: oldNode, NewNode: newNode, Workspaces: workspaces}
	return plan, plan.Validate(false)
}

// CommitNativeRestore is the single authority switch. The replacement's
// detached files have already been checked by that node's storage owner.
func (s *Store) CommitNativeRestore(ctx context.Context, plan nativebackup.Plan, token string, now time.Time) error {
	if err := plan.Validate(true); err != nil {
		return err
	}
	request := adminkernel.NormalizeAdmitNodeRequest(adminkernel.AdmitNodeRequest{
		NodeID: plan.NewNode, EnrollmentToken: token, OperatorReason: "native restore from " + plan.OldNode, Now: now,
	})
	if err := adminkernel.ValidateAdmitNodeRequest(request); err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(request.EnrollmentToken))
	tokenHash := hex.EncodeToString(sum[:])
	tx, err := s.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	applied, err := nativeRestoreFence(ctx, tx, plan.OldNode, plan.NewNode, tokenHash)
	if err != nil {
		return err
	}
	node := plan.OldNode
	if applied {
		node = plan.NewNode
	}
	current, err := nativeRestoreInventory(ctx, tx, node)
	if err != nil {
		return err
	}
	expected := slices.Clone(plan.Workspaces)
	for i := range expected {
		if applied {
			expected[i].OldIdentity = expected[i].NewIdentity
		}
		expected[i].NewIdentity = ""
	}
	if !slices.Equal(current, expected) {
		return errors.New("native restore inventory changed before committing the Node mapping")
	}
	if applied {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE nodes SET lifecycle_status='revoked' WHERE node_id=$1`, plan.OldNode); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,lifecycle_status)
		VALUES($1,'',$2,$3,'active')`, plan.NewNode, tokenHash, now); err != nil {
		return err
	}
	for _, item := range plan.Workspaces {
		if _, err := tx.Exec(ctx, `UPDATE workspaces SET node_id=$2,storage_identity=$3,updated_at=$4,
			next_reconcile_at=$4,claim_owner='',claim_expires_at=NULL WHERE workspace_id=$1`,
			item.ID, plan.NewNode, item.NewIdentity, now); err != nil {
			return err
		}
	}
	for _, audit := range []adminAuditEvent{
		{EventID: "admaudit-" + uuid.NewString(), Operation: adminkernel.AuditOperationRevokeNode, TargetType: adminkernel.AuditTargetNode,
			TargetID: plan.OldNode, OperatorReason: "native restore to " + plan.NewNode, CreatedAt: now},
		{EventID: "admaudit-" + uuid.NewString(), Operation: adminkernel.AuditOperationAdmitNode, TargetType: adminkernel.AuditTargetNode,
			TargetID: plan.NewNode, OperatorReason: request.OperatorReason, CreatedAt: now},
	} {
		if err := insertAdminAuditEvent(ctx, tx, audit); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func nativeRestoreFence(ctx context.Context, tx pgx.Tx, oldNode, newNode, tokenHash string) (bool, error) {
	var oldLifecycle, newLifecycle, newToken, newTarget string
	if err := tx.QueryRow(ctx, `SELECT lifecycle_status FROM nodes WHERE node_id=$1 FOR UPDATE`, oldNode).Scan(&oldLifecycle); err != nil {
		return false, fmt.Errorf("load source Node: %w", err)
	}
	err := tx.QueryRow(ctx, `SELECT lifecycle_status,enrollment_token_hash,node_target FROM nodes WHERE node_id=$1 FOR UPDATE`, newNode).
		Scan(&newLifecycle, &newToken, &newTarget)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	applied := err == nil && oldLifecycle == "revoked" && newLifecycle == "active" && newTarget == "" &&
		tokenHash != "" && newToken == tokenHash
	if !applied && !(oldLifecycle == "active" && errors.Is(err, pgx.ErrNoRows)) {
		return false, errors.New("native restore requires the original active Node and a fresh replacement identity")
	}
	node := oldNode
	if applied {
		node = newNode
	}
	return applied, requireNativeWorkDrained(ctx, tx, node)
}

// RequireNativeWorkDrained verifies the stopped installation's durable
// authority before any target migrations can run.
func (s *Store) RequireNativeWorkDrained(ctx context.Context, node string) error {
	tx, err := s.db.Pool().BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	return requireNativeWorkDrained(ctx, tx, node)
}

func requireNativeWorkDrained(ctx context.Context, tx pgx.Tx, node string) error {
	var outstanding bool
	if err := tx.QueryRow(ctx, `SELECT
		NOT EXISTS(SELECT 1 FROM nodes WHERE lifecycle_status='active' AND node_id=$1) OR
		EXISTS(SELECT 1 FROM nodes WHERE lifecycle_status='active' AND node_id<>$1) OR
		EXISTS(SELECT 1 FROM runs WHERE status NOT IN ('RUN_STATUS_SUCCEEDED','RUN_STATUS_FAILED','RUN_STATUS_CANCELLED')) OR
		EXISTS(SELECT 1 FROM allocations WHERE lifecycle_state<>'ALLOCATION_LIFECYCLE_STATE_RELEASED') OR
		EXISTS(SELECT 1 FROM workspaces WHERE status<>'WORKSPACE_STATUS_DELETED' AND
			(node_id<>$1 OR status<>'WORKSPACE_STATUS_READY' OR attached_allocation_id IS NOT NULL))`, node).Scan(&outstanding); err != nil {
		return err
	}
	if outstanding {
		return errors.New("native maintenance requires one original Node and fully drained execution and Workspace state")
	}
	return nil
}

func nativeRestoreInventory(ctx context.Context, tx pgx.Tx, node string) ([]nativebackup.Workspace, error) {
	rows, err := tx.Query(ctx, `SELECT workspace_id,namespace,storage_root_identity,storage_identity,
		capacity_bytes,inode_limit,generation FROM workspaces WHERE node_id=$1 AND status='WORKSPACE_STATUS_READY'
		ORDER BY workspace_id COLLATE "C" FOR UPDATE`, node)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	inventory := []nativebackup.Workspace{}
	for rows.Next() {
		var item nativebackup.Workspace
		if err := rows.Scan(&item.ID, &item.Namespace, &item.RootIdentity, &item.OldIdentity,
			&item.Bytes, &item.Inodes, &item.Generation); err != nil {
			return nil, err
		}
		inventory = append(inventory, item)
	}
	return inventory, rows.Err()
}
