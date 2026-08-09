package pgadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

func (s *Store) GetNodeCapabilitySnapshot(ctx context.Context, nodeID string) (*capabilityv1.CapabilitySnapshot, error) {
	var payload []byte
	err := s.db.Pool().QueryRow(ctx, `SELECT summary FROM node_summaries WHERE node_id = $1`, strings.TrimSpace(nodeID)).Scan(&payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "node capability snapshot not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load node capability snapshot: %w", err)
	}
	summary := &nodev1.NodeSummary{}
	if err := protojson.Unmarshal(payload, summary); err != nil {
		return nil, fmt.Errorf("unmarshal node summary: %w", err)
	}
	if summary.GetCapabilitySnapshot() == nil {
		return nil, grpcstatus.Error(codes.NotFound, "node capability snapshot not found")
	}
	return summary.GetCapabilitySnapshot(), nil
}

func (s *Store) ListNodeCapabilityTransitions(ctx context.Context, nodeID string, limit int32) ([]adminkernel.CapabilityTransition, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT transition_id, node_id, snapshot_id, snapshot_sequence, capability_key,
		       old_state, new_state, old_evidence_id, new_evidence_id, reason_code,
		       reason, observed_at, reported_at
		FROM node_capability_transitions
		WHERE ($1 = '' OR node_id = $1)
		ORDER BY reported_at DESC, transition_id DESC LIMIT $2
	`, strings.TrimSpace(nodeID), limit)
	if err != nil {
		return nil, fmt.Errorf("list node capability transitions: %w", err)
	}
	defer rows.Close()
	var result []adminkernel.CapabilityTransition
	for rows.Next() {
		var item adminkernel.CapabilityTransition
		var keyJSON []byte
		var oldState, newState, reasonCode string
		if err := rows.Scan(&item.TransitionID, &item.NodeID, &item.SnapshotID, &item.SnapshotSequence, &keyJSON,
			&oldState, &newState, &item.OldEvidenceID, &item.NewEvidenceID, &reasonCode,
			&item.Reason, &item.ObservedAt, &item.ReportedAt); err != nil {
			return nil, fmt.Errorf("scan node capability transition: %w", err)
		}
		item.Key = &capabilityv1.CapabilityKey{}
		if err := protojson.Unmarshal(keyJSON, item.Key); err != nil {
			return nil, fmt.Errorf("unmarshal transition capability key: %w", err)
		}
		oldValue, ok := capabilityv1.CapabilityState_value[oldState]
		if !ok {
			return nil, fmt.Errorf("unknown stored old capability state %q", oldState)
		}
		newValue, ok := capabilityv1.CapabilityState_value[newState]
		if !ok {
			return nil, fmt.Errorf("unknown stored new capability state %q", newState)
		}
		reasonValue, ok := capabilityv1.CapabilityReasonCode_value[reasonCode]
		if !ok {
			return nil, fmt.Errorf("unknown stored capability reason %q", reasonCode)
		}
		item.OldState = capabilityv1.CapabilityState(oldValue)
		item.NewState = capabilityv1.CapabilityState(newValue)
		item.ReasonCode = capabilityv1.CapabilityReasonCode(reasonValue)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListCapabilityReconcileQueue(ctx context.Context, nodeID string, limit int32) ([]adminkernel.CapabilityReconcileItem, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT q.allocation_id, a.node_id, q.pending_dependencies, q.reconcile_attempts,
		       q.next_run_at, q.lease_expires_at, q.last_error, q.updated_at
		FROM allocation_capability_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		WHERE ($1 = '' OR a.node_id = $1)
		ORDER BY q.next_run_at, q.allocation_id LIMIT $2
	`, strings.TrimSpace(nodeID), limit)
	if err != nil {
		return nil, fmt.Errorf("list capability reconcile queue: %w", err)
	}
	defer rows.Close()
	var result []adminkernel.CapabilityReconcileItem
	for rows.Next() {
		var item adminkernel.CapabilityReconcileItem
		var dependenciesJSON []byte
		if err := rows.Scan(&item.AllocationID, &item.NodeID, &dependenciesJSON, &item.Attempts,
			&item.NextRunAt, &item.LeaseExpiresAt, &item.LastError, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan capability reconcile queue: %w", err)
		}
		set := &capabilityv1.CapabilityDependencySet{}
		if err := protojson.Unmarshal(dependenciesJSON, set); err != nil {
			return nil, fmt.Errorf("unmarshal queued capability dependencies: %w", err)
		}
		item.Dependencies = set.GetDependencies()
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) GetAllocationCapabilityDiagnostics(ctx context.Context, allocationID string) (*adminkernel.AllocationCapabilityDiagnostics, error) {
	var nodeID string
	var dependenciesJSON, evidenceJSON, conditionsJSON []byte
	err := s.db.Pool().QueryRow(ctx, `
		SELECT node_id, capability_dependencies, admitted_capability_dependencies, capability_conditions
		FROM allocations WHERE allocation_id = $1
	`, strings.TrimSpace(allocationID)).Scan(&nodeID, &dependenciesJSON, &evidenceJSON, &conditionsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load allocation capability diagnostics: %w", err)
	}
	dependencies := &capabilityv1.CapabilityDependencySet{}
	admitted := &capabilityv1.CapabilityDependencySet{}
	conditions := &capabilityv1.CapabilityConditionSet{}
	if err := protojson.Unmarshal(dependenciesJSON, dependencies); err != nil {
		return nil, fmt.Errorf("unmarshal capability dependencies: %w", err)
	}
	if err := protojson.Unmarshal(evidenceJSON, admitted); err != nil {
		return nil, fmt.Errorf("unmarshal admitted capability evidence: %w", err)
	}
	if err := protojson.Unmarshal(conditionsJSON, conditions); err != nil {
		return nil, fmt.Errorf("unmarshal capability conditions: %w", err)
	}
	result := &adminkernel.AllocationCapabilityDiagnostics{
		AllocationID: allocationID, NodeID: nodeID, Dependencies: dependencies.GetDependencies(),
		AdmittedDependencies: admitted.GetDependencies(), Conditions: conditions.GetConditions(),
	}
	var item adminkernel.CapabilityReconcileItem
	var queuedDependencies []byte
	err = s.db.Pool().QueryRow(ctx, `
		SELECT q.allocation_id, a.node_id, q.pending_dependencies, q.reconcile_attempts,
		       q.next_run_at, q.lease_expires_at, q.last_error, q.updated_at
		FROM allocation_capability_reconcile_queue q
		JOIN allocations a ON a.allocation_id = q.allocation_id
		WHERE q.allocation_id = $1
	`, strings.TrimSpace(allocationID)).Scan(&item.AllocationID, &item.NodeID, &queuedDependencies, &item.Attempts,
		&item.NextRunAt, &item.LeaseExpiresAt, &item.LastError, &item.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load allocation capability reconcile state: %w", err)
	}
	if err == nil {
		queued := &capabilityv1.CapabilityDependencySet{}
		if err := protojson.Unmarshal(queuedDependencies, queued); err != nil {
			return nil, fmt.Errorf("unmarshal queued allocation dependencies: %w", err)
		}
		item.Dependencies = queued.GetDependencies()
		result.Reconcile = &item
	}
	return result, nil
}
