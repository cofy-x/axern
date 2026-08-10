package pgadmin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	adminkernel "github.com/cofy-x/axern/control/controld/internal/kernel/admin"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		       old_state, new_state, old_evidence, new_evidence, old_reason_code, new_reason_code,
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
		var keyJSON, oldEvidenceJSON, newEvidenceJSON []byte
		var oldState, newState, oldReason, newReason string
		if err := rows.Scan(&item.TransitionID, &item.NodeID, &item.SnapshotID, &item.SnapshotSequence, &keyJSON,
			&oldState, &newState, &oldEvidenceJSON, &newEvidenceJSON, &oldReason, &newReason,
			&item.Reason, &item.ObservedAt, &item.ReportedAt); err != nil {
			return nil, fmt.Errorf("scan node capability transition: %w", err)
		}
		item.Key = &capabilityv1.CapabilityKey{}
		if err := protojson.Unmarshal(keyJSON, item.Key); err != nil {
			return nil, fmt.Errorf("unmarshal transition capability key: %w", err)
		}
		if item.OldState, err = parseCapabilityState(oldState); err != nil {
			return nil, err
		}
		if item.NewState, err = parseCapabilityState(newState); err != nil {
			return nil, err
		}
		if item.OldReasonCode, err = parseCapabilityReason(oldReason); err != nil {
			return nil, err
		}
		if item.NewReasonCode, err = parseCapabilityReason(newReason); err != nil {
			return nil, err
		}
		if item.OldEvidence, err = unmarshalEvidence(oldEvidenceJSON); err != nil {
			return nil, err
		}
		if item.NewEvidence, err = unmarshalEvidence(newEvidenceJSON); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ListCapabilityReconcileQueue(ctx context.Context, nodeID string, limit int32) ([]adminkernel.CapabilityReconcileItem, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Pool().Query(ctx, capabilityQueueSelect+`
		WHERE ($1 = '' OR a.node_id = $1)
		ORDER BY q.next_run_at, q.allocation_id LIMIT $2
	`, strings.TrimSpace(nodeID), limit)
	if err != nil {
		return nil, fmt.Errorf("list capability reconcile queue: %w", err)
	}
	defer rows.Close()
	var result []adminkernel.CapabilityReconcileItem
	for rows.Next() {
		item, err := scanCapabilityReconcileItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *item)
	}
	return result, rows.Err()
}

func (s *Store) GetAllocationCapabilityDiagnostics(ctx context.Context, allocationID string) (*adminkernel.AllocationCapabilityDiagnostics, error) {
	allocationID = strings.TrimSpace(allocationID)
	result := &adminkernel.AllocationCapabilityDiagnostics{AllocationID: allocationID}
	if err := s.db.Pool().QueryRow(ctx, `SELECT node_id, attempt FROM allocations WHERE allocation_id = $1`, allocationID).Scan(&result.NodeID, &result.Attempt); errors.Is(err, pgx.ErrNoRows) {
		return nil, grpcstatus.Error(codes.NotFound, "allocation not found")
	} else if err != nil {
		return nil, fmt.Errorf("load allocation capability diagnostics: %w", err)
	}
	var admittedAt time.Time
	if err := s.db.Pool().QueryRow(ctx, `
		SELECT dependency_set_digest, admitted_at
		FROM allocation_capability_admissions
		WHERE allocation_id = $1 AND allocation_attempt = $2
	`, allocationID, result.Attempt).Scan(&result.CreateDependencySetDigest, &admittedAt); err == nil {
		result.CreateAdmissionRecorded = true
		result.CreateAdmittedAt = &admittedAt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load immutable allocation capability admission: %w", err)
	}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT placement_dependency, admitted_dependency
		FROM allocation_capability_dependencies
		WHERE allocation_id = $1 ORDER BY capability_key_id
	`, allocationID)
	if err != nil {
		return nil, fmt.Errorf("load allocation capability dependencies: %w", err)
	}
	for rows.Next() {
		var placementJSON []byte
		var admittedJSON []byte
		if err := rows.Scan(&placementJSON, &admittedJSON); err != nil {
			return nil, err
		}
		placement, err := unmarshalDependency(placementJSON)
		if err != nil {
			return nil, err
		}
		result.Dependencies = append(result.Dependencies, placement)
		if len(admittedJSON) > 0 {
			admitted, err := unmarshalDependency(admittedJSON)
			if err != nil {
				return nil, err
			}
			result.AdmittedDependencies = append(result.AdmittedDependencies, admitted)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err := s.loadCapabilityConditionSet(ctx, allocationID, result.Attempt, result); err != nil {
		return nil, err
	}
	row := s.db.Pool().QueryRow(ctx, capabilityQueueSelect+` WHERE q.allocation_id = $1`, allocationID)
	item, err := scanCapabilityReconcileItem(row)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("load allocation capability reconcile state: %w", err)
	}
	if err == nil {
		result.Reconcile = item
	}
	return result, nil
}

func (s *Store) loadCapabilityConditionSet(ctx context.Context, allocationID string, attempt int64, result *adminkernel.AllocationCapabilityDiagnostics) error {
	var revision int64
	var observedAt time.Time
	if err := s.db.Pool().QueryRow(ctx, `
		SELECT revision, observed_at FROM allocation_capability_condition_sets WHERE allocation_id = $1 AND allocation_attempt = $2
	`, allocationID, attempt).Scan(&revision, &observedAt); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("load allocation capability condition set: %w", err)
	}
	set := &capabilityv1.CapabilityConditionSet{Revision: revision, ObservedAt: timestamppb.New(observedAt)}
	rows, err := s.db.Pool().Query(ctx, `
		SELECT condition FROM allocation_capability_conditions
		WHERE allocation_id = $1 AND allocation_attempt = $2 ORDER BY capability_key_id
	`, allocationID, attempt)
	if err != nil {
		return fmt.Errorf("load allocation capability conditions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return err
		}
		condition := &capabilityv1.CapabilityCondition{}
		if err := protojson.Unmarshal(payload, condition); err != nil {
			return fmt.Errorf("unmarshal allocation capability condition: %w", err)
		}
		set.Conditions = append(set.Conditions, condition)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	result.ConditionSet = set
	return nil
}

const capabilityQueueSelect = `
	SELECT q.allocation_id, a.node_id,
	       jsonb_build_object('dependencies', COALESCE((
		       SELECT jsonb_agg(COALESCE(d.admitted_dependency, d.placement_dependency) ORDER BY p.capability_key_id)
		       FROM allocation_capability_reconcile_pending_keys p
		       JOIN allocation_capability_dependencies d
		         ON d.allocation_id = p.allocation_id AND d.capability_key_id = p.capability_key_id
		       WHERE p.allocation_id = q.allocation_id
	       ), '[]'::jsonb)),
	       q.reconcile_attempts, q.next_run_at, q.lease_expires_at, q.last_error, q.updated_at
	FROM allocation_capability_reconcile_queue q
	JOIN allocations a ON a.allocation_id = q.allocation_id
`

type rowScanner interface{ Scan(...any) error }

func scanCapabilityReconcileItem(row rowScanner) (*adminkernel.CapabilityReconcileItem, error) {
	var item adminkernel.CapabilityReconcileItem
	var dependenciesJSON []byte
	if err := row.Scan(&item.AllocationID, &item.NodeID, &dependenciesJSON, &item.Attempts,
		&item.NextRunAt, &item.LeaseExpiresAt, &item.LastError, &item.UpdatedAt); err != nil {
		return nil, err
	}
	set := &capabilityv1.CapabilityDependencySet{}
	if err := protojson.Unmarshal(dependenciesJSON, set); err != nil {
		return nil, fmt.Errorf("unmarshal queued capability dependencies: %w", err)
	}
	item.Dependencies = set.GetDependencies()
	return &item, nil
}

func unmarshalDependency(payload []byte) (*capabilityv1.CapabilityDependency, error) {
	dependency := &capabilityv1.CapabilityDependency{}
	if err := protojson.Unmarshal(payload, dependency); err != nil {
		return nil, fmt.Errorf("unmarshal allocation capability dependency: %w", err)
	}
	return dependency, nil
}

func unmarshalEvidence(payload []byte) (*capabilityv1.CapabilityEvidence, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil, nil
	}
	evidence := &capabilityv1.CapabilityEvidence{}
	if err := protojson.Unmarshal(payload, evidence); err != nil {
		return nil, fmt.Errorf("unmarshal capability evidence: %w", err)
	}
	return evidence, nil
}

func parseCapabilityState(value string) (capabilityv1.CapabilityState, error) {
	number, ok := capabilityv1.CapabilityState_value[value]
	if !ok {
		return capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED, fmt.Errorf("unknown stored capability state %q", value)
	}
	return capabilityv1.CapabilityState(number), nil
}

func parseCapabilityReason(value string) (capabilityv1.CapabilityReasonCode, error) {
	number, ok := capabilityv1.CapabilityReasonCode_value[value]
	if !ok {
		return capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_UNSPECIFIED, fmt.Errorf("unknown stored capability reason %q", value)
	}
	return capabilityv1.CapabilityReasonCode(number), nil
}
