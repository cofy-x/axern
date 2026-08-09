package pgallocation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
)

type CapabilityQueue struct {
	db *postgres.DB
}

func NewCapabilityQueue(db *postgres.DB) *CapabilityQueue { return &CapabilityQueue{db: db} }

func (q *CapabilityQueue) Claim(ctx context.Context, owner string, limit int, now time.Time, leaseTTL time.Duration) ([]allocationkernel.CapabilityReconcileItem, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, fmt.Errorf("claim capability reconciliation: owner is required")
	}
	if leaseTTL <= 0 {
		return nil, fmt.Errorf("claim capability reconciliation: lease TTL must be positive")
	}
	if limit <= 0 {
		limit = 32
	}
	rows, err := q.db.Pool().Query(ctx, `
		WITH candidates AS (
			SELECT q.allocation_id
			FROM allocation_capability_reconcile_queue q
			WHERE q.next_run_at <= $1 AND (q.lease_expires_at IS NULL OR q.lease_expires_at <= $1)
			ORDER BY q.next_run_at, q.allocation_id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE allocation_capability_reconcile_queue q
			SET lease_owner = $3, lease_expires_at = $4, updated_at = $1
			FROM candidates c WHERE q.allocation_id = c.allocation_id
			RETURNING q.allocation_id, q.pending_dependencies, q.reconcile_attempts
		)
		SELECT c.allocation_id, a.node_id, n.node_target, a.attempt, c.pending_dependencies, c.reconcile_attempts
		FROM claimed c
		JOIN allocations a ON a.allocation_id = c.allocation_id
		JOIN nodes n ON n.node_id = a.node_id
		ORDER BY c.allocation_id
	`, now.UTC(), limit, owner, now.Add(leaseTTL).UTC())
	if err != nil {
		return nil, fmt.Errorf("claim capability reconcile queue: %w", err)
	}
	defer rows.Close()
	var items []allocationkernel.CapabilityReconcileItem
	for rows.Next() {
		var item allocationkernel.CapabilityReconcileItem
		var payload []byte
		if err := rows.Scan(&item.AllocationID, &item.NodeID, &item.NodeTarget, &item.Attempt, &payload, &item.Attempts); err != nil {
			return nil, err
		}
		set := &capabilityv1.CapabilityDependencySet{}
		if err := protojson.Unmarshal(payload, set); err != nil {
			return nil, fmt.Errorf("unmarshal queued capability dependencies: %w", err)
		}
		item.Dependencies = set.GetDependencies()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *CapabilityQueue) Complete(ctx context.Context, allocationID, owner string) error {
	tag, err := q.db.Pool().Exec(ctx, `DELETE FROM allocation_capability_reconcile_queue WHERE allocation_id = $1 AND lease_owner = $2`, strings.TrimSpace(allocationID), strings.TrimSpace(owner))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("complete capability reconciliation for %q: lease is no longer owned", allocationID)
	}
	return nil
}

func (q *CapabilityQueue) Retry(ctx context.Context, item allocationkernel.CapabilityReconcileItem, owner string, now time.Time, reconcileErr error) error {
	if reconcileErr == nil {
		return fmt.Errorf("retry capability reconciliation: error is required")
	}
	delay := time.Duration(1<<min(item.Attempts, 8)) * time.Second
	tag, err := q.db.Pool().Exec(ctx, `
		UPDATE allocation_capability_reconcile_queue
		SET reconcile_attempts = reconcile_attempts + 1, last_error = $3, next_run_at = $4,
			lease_owner = '', lease_expires_at = NULL, updated_at = $5
		WHERE allocation_id = $1 AND lease_owner = $2
	`, item.AllocationID, strings.TrimSpace(owner), strings.TrimSpace(reconcileErr.Error()), now.Add(delay).UTC(), now.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("retry capability reconciliation for %q: lease is no longer owned", item.AllocationID)
	}
	return nil
}

func (q *CapabilityQueue) RecordAdmission(ctx context.Context, item allocationkernel.CapabilityReconcileItem, owner string, admission *allocationkernel.CapabilityAdmission, now time.Time) error {
	tx, err := q.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin capability reconciliation verification: %w", err)
	}
	defer tx.Rollback(ctx)
	var leased bool
	err = tx.QueryRow(ctx, `
		SELECT TRUE
		FROM allocation_capability_reconcile_queue
		WHERE allocation_id = $1 AND lease_owner = $2 AND lease_expires_at > $3
		FOR UPDATE
	`, item.AllocationID, strings.TrimSpace(owner), now.UTC()).Scan(&leased)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("record capability reconciliation for %q: lease is no longer owned", item.AllocationID)
	}
	if err != nil {
		return fmt.Errorf("lock capability reconciliation lease for %q: %w", item.AllocationID, err)
	}
	if !leased {
		return fmt.Errorf("record capability reconciliation for %q: invalid lease", item.AllocationID)
	}
	if err := RecordCapabilityVerification(ctx, tx, item.AllocationID, admission, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit capability reconciliation verification: %w", err)
	}
	return nil
}
