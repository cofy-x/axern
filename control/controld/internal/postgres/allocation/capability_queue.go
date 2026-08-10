package pgallocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
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
			RETURNING q.allocation_id, q.reconcile_attempts
		)
		SELECT c.allocation_id, a.node_id, n.node_target, a.attempt,
			jsonb_build_object('dependencies', COALESCE((
				SELECT jsonb_agg(COALESCE(d.admitted_dependency, d.placement_dependency) ORDER BY p.capability_key_id)
				FROM allocation_capability_reconcile_pending_keys p
				JOIN allocation_capability_dependencies d
				  ON d.allocation_id = p.allocation_id AND d.capability_key_id = p.capability_key_id
				WHERE p.allocation_id = c.allocation_id
			), '[]'::jsonb)),
			COALESCE((
				SELECT jsonb_object_agg(p.capability_key_id, p.snapshot_sequence)
				FROM allocation_capability_reconcile_pending_keys p
				WHERE p.allocation_id = c.allocation_id
			), '{}'::jsonb),
			c.reconcile_attempts
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
		var dependencyPayload, generationPayload []byte
		if err := rows.Scan(&item.AllocationID, &item.NodeID, &item.NodeTarget, &item.Attempt, &dependencyPayload, &generationPayload, &item.Attempts); err != nil {
			return nil, err
		}
		set := &capabilityv1.CapabilityDependencySet{}
		if err := protojson.Unmarshal(dependencyPayload, set); err != nil {
			return nil, fmt.Errorf("unmarshal queued capability dependencies: %w", err)
		}
		if err := json.Unmarshal(generationPayload, &item.PendingGenerations); err != nil {
			return nil, fmt.Errorf("unmarshal queued capability generations: %w", err)
		}
		if len(item.PendingGenerations) == 0 {
			return nil, fmt.Errorf("capability reconcile item %q has no pending generations", item.AllocationID)
		}
		item.Dependencies = set.GetDependencies()
		if err := validateClaimedCapabilityWork(item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (q *CapabilityQueue) Complete(ctx context.Context, item allocationkernel.CapabilityReconcileItem, owner string, now time.Time) error {
	allocationID := strings.TrimSpace(item.AllocationID)
	owner = strings.TrimSpace(owner)
	if allocationID == "" || owner == "" || len(item.PendingGenerations) == 0 {
		return fmt.Errorf("complete capability reconciliation: allocation, owner, and claimed generations are required")
	}
	tx, err := q.db.Pool().Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin capability reconcile completion: %w", err)
	}
	defer tx.Rollback(ctx)
	var allocationExists bool
	err = tx.QueryRow(ctx, `
		SELECT TRUE FROM allocation_capability_reconcile_queue
		WHERE allocation_id = $1 AND lease_owner = $2 AND lease_expires_at > $3
		FOR UPDATE
	`, allocationID, owner, now.UTC()).Scan(&allocationExists)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("complete capability reconciliation for %q: lease is no longer owned", allocationID)
	}
	if err != nil {
		return fmt.Errorf("lock capability reconciliation completion for %q: %w", allocationID, err)
	}
	keyIDs := make([]string, 0, len(item.PendingGenerations))
	for keyID := range item.PendingGenerations {
		keyIDs = append(keyIDs, keyID)
	}
	sort.Strings(keyIDs)
	for _, keyID := range keyIDs {
		if _, err := tx.Exec(ctx, `
			DELETE FROM allocation_capability_reconcile_pending_keys
			WHERE allocation_id = $1 AND capability_key_id = $2 AND snapshot_sequence <= $3
		`, allocationID, keyID, item.PendingGenerations[keyID]); err != nil {
			return fmt.Errorf("ack capability generation %q for %q: %w", keyID, allocationID, err)
		}
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM allocation_capability_reconcile_pending_keys WHERE allocation_id = $1`, allocationID).Scan(&remaining); err != nil {
		return fmt.Errorf("count remaining capability generations for %q: %w", allocationID, err)
	}
	if remaining == 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM allocation_capability_reconcile_queue WHERE allocation_id = $1`, allocationID); err != nil {
			return fmt.Errorf("complete capability reconciliation for %q: %w", allocationID, err)
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE allocation_capability_reconcile_queue
		SET next_run_at = LEAST(next_run_at, $2), lease_owner = '', lease_expires_at = NULL,
			last_error = '', updated_at = $2
		WHERE allocation_id = $1
	`, allocationID, now.UTC()); err != nil {
		return fmt.Errorf("release capability reconciliation with newer work for %q: %w", allocationID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit capability reconcile completion for %q: %w", allocationID, err)
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
		WHERE allocation_id = $1 AND lease_owner = $2 AND lease_expires_at > $5
	`, item.AllocationID, strings.TrimSpace(owner), capabilitycontract.BoundedReason(strings.TrimSpace(reconcileErr.Error())), now.Add(delay).UTC(), now.UTC())
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("retry capability reconciliation for %q: lease is no longer owned", item.AllocationID)
	}
	return nil
}

func validateClaimedCapabilityWork(item allocationkernel.CapabilityReconcileItem) error {
	dependencyKeys := make(map[string]struct{}, len(item.Dependencies))
	for _, dependency := range item.Dependencies {
		keyID, err := capabilitycontract.KeyID(dependency.GetKey())
		if err != nil {
			return fmt.Errorf("capability reconcile item %q has malformed dependency: %w", item.AllocationID, err)
		}
		if _, duplicate := dependencyKeys[keyID]; duplicate {
			return fmt.Errorf("capability reconcile item %q has duplicate dependency %q", item.AllocationID, keyID)
		}
		dependencyKeys[keyID] = struct{}{}
	}
	if len(dependencyKeys) != len(item.PendingGenerations) {
		return fmt.Errorf("capability reconcile item %q has %d pending generations but %d durable dependencies", item.AllocationID, len(item.PendingGenerations), len(dependencyKeys))
	}
	for keyID := range item.PendingGenerations {
		if _, ok := dependencyKeys[keyID]; !ok {
			return fmt.Errorf("capability reconcile item %q is missing durable dependency %q", item.AllocationID, keyID)
		}
	}
	return nil
}

func (q *CapabilityQueue) RecordConditions(ctx context.Context, item allocationkernel.CapabilityReconcileItem, owner string, reconciliation *allocationkernel.CapabilityReconciliation, now time.Time) error {
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
	if reconciliation == nil || reconciliation.Attempt != item.Attempt || reconciliation.ConditionSet == nil {
		return fmt.Errorf("record capability reconciliation for %q: attempt and full condition set are required", item.AllocationID)
	}
	durableDependencies, err := LoadCapabilityDependencies(ctx, tx, item.AllocationID)
	if err != nil {
		return err
	}
	if !sameDependencyProofs(durableDependencies, reconciliation.Dependencies) {
		return fmt.Errorf("record capability reconciliation for %q: node returned dependencies that differ from immutable create proof", item.AllocationID)
	}
	if err := ReplaceCapabilityConditions(ctx, tx, item.AllocationID, reconciliation.ConditionSet, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit capability reconciliation verification: %w", err)
	}
	return nil
}
