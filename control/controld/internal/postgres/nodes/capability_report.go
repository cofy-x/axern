package pgnodes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type capabilityTransition struct {
	snapshotSequence int64
	key              *capabilityv1.CapabilityKey
	keyID            string
	oldState         capabilityv1.CapabilityState
	newState         capabilityv1.CapabilityState
	oldEvidence      *capabilityv1.CapabilityEvidence
	newEvidence      *capabilityv1.CapabilityEvidence
	oldReasonCode    capabilityv1.CapabilityReasonCode
	newReasonCode    capabilityv1.CapabilityReasonCode
	reason           string
	observedAt       time.Time
}

func persistCapabilityReport(ctx context.Context, tx pgx.Tx, nodeID string, previous, next *nodev1.NodeSummary, reportedAt time.Time) ([]capabilityTransition, error) {
	nextSnapshot := next.GetCapabilitySnapshot()
	if err := nodecapability.ValidateSnapshot(nextSnapshot, reportedAt); err != nil {
		return nil, fmt.Errorf("validate capability snapshot: %w", err)
	}
	previousSnapshot := previous.GetCapabilitySnapshot()
	if err := persistCapabilityInstance(ctx, tx, nodeID, previousSnapshot, nextSnapshot, reportedAt); err != nil {
		return nil, err
	}
	idempotent, err := validateSnapshotAdvance(previousSnapshot, nextSnapshot)
	if err != nil {
		return nil, err
	}
	if idempotent {
		return nil, nil
	}
	transitions, err := capabilityTransitions(previousSnapshot, nextSnapshot, reportedAt)
	if err != nil {
		return nil, err
	}
	for _, transition := range transitions {
		keyJSON, err := protojson.Marshal(transition.key)
		if err != nil {
			return nil, fmt.Errorf("marshal capability key %q: %w", transition.keyID, err)
		}
		transitionID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(nodeID+"\x00"+nextSnapshot.GetSnapshotID()+"\x00"+transition.keyID)).String()
		oldEvidenceJSON, err := marshalNullableCapabilityEvidence(transition.oldEvidence)
		if err != nil {
			return nil, fmt.Errorf("marshal old capability evidence %q: %w", transition.keyID, err)
		}
		newEvidenceJSON, err := marshalNullableCapabilityEvidence(transition.newEvidence)
		if err != nil {
			return nil, fmt.Errorf("marshal new capability evidence %q: %w", transition.keyID, err)
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO node_capability_transitions (
				transition_id, node_id, snapshot_id, snapshot_sequence, capability_key, capability_key_id,
				old_state, new_state, old_evidence, new_evidence,
				old_reason_code, new_reason_code, reason, observed_at, reported_at
			) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12, $13, $14, $15)
			ON CONFLICT (node_id, snapshot_id, capability_key_id) DO NOTHING
		`, transitionID, nodeID, nextSnapshot.GetSnapshotID(), nextSnapshot.GetSequence(), string(keyJSON), transition.keyID,
			transition.oldState.String(), transition.newState.String(), string(oldEvidenceJSON), string(newEvidenceJSON),
			transition.oldReasonCode.String(), transition.newReasonCode.String(),
			transition.reason, transition.observedAt, reportedAt.UTC())
		if err != nil {
			return nil, fmt.Errorf("insert capability transition %q: %w", transition.keyID, err)
		}
		// An exact report replay returns before transition generation. Reaching
		// this conflict therefore means a node reused an older snapshot identity
		// for a different sequence or payload. Failing the report preserves the
		// transition journal instead of silently losing history.
		if tag.RowsAffected() != 1 {
			return nil, fmt.Errorf("capability snapshot identity %q was already used for capability %q", nextSnapshot.GetSnapshotID(), transition.keyID)
		}
	}
	if len(transitions) == 0 {
		return nil, nil
	}
	if err := enqueueAffectedAllocations(ctx, tx, nodeID, transitions, reportedAt); err != nil {
		return nil, err
	}
	return transitions, nil
}

func marshalNullableCapabilityEvidence(evidence *capabilityv1.CapabilityEvidence) ([]byte, error) {
	if evidence == nil {
		return []byte("null"), nil
	}
	return protojson.Marshal(evidence)
}

func persistCapabilityInstance(ctx context.Context, tx pgx.Tx, nodeID string, previous, next *capabilityv1.CapabilitySnapshot, reportedAt time.Time) error {
	var lastSequence int64
	err := tx.QueryRow(ctx, `
		SELECT last_sequence
		FROM node_capability_instances
		WHERE node_id = $1 AND node_instance_id = $2
		FOR UPDATE
	`, nodeID, next.GetNodeInstanceID()).Scan(&lastSequence)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load node capability instance: %w", err)
	}
	instanceKnown := err == nil
	if previous != nil && previous.GetNodeInstanceID() != next.GetNodeInstanceID() && instanceKnown {
		return fmt.Errorf("capability node instance %q was already superseded and cannot become active again", next.GetNodeInstanceID())
	}
	if previous == nil && instanceKnown {
		return fmt.Errorf("capability node instance %q exists without a current node summary", next.GetNodeInstanceID())
	}
	if instanceKnown && next.GetSequence() < lastSequence {
		return fmt.Errorf("capability node instance %q sequence regressed below durable sequence %d", next.GetNodeInstanceID(), lastSequence)
	}
	if !instanceKnown {
		if _, err := tx.Exec(ctx, `
			INSERT INTO node_capability_instances (
				node_id, node_instance_id, first_snapshot_id, last_snapshot_id,
				last_sequence, first_seen_at, last_seen_at
			) VALUES ($1, $2, $3, $3, $4, $5, $5)
		`, nodeID, next.GetNodeInstanceID(), next.GetSnapshotID(), next.GetSequence(), reportedAt.UTC()); err != nil {
			return fmt.Errorf("insert node capability instance: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE node_capability_instances
		SET last_snapshot_id = $3, last_sequence = $4, last_seen_at = $5
		WHERE node_id = $1 AND node_instance_id = $2
	`, nodeID, next.GetNodeInstanceID(), next.GetSnapshotID(), next.GetSequence(), reportedAt.UTC()); err != nil {
		return fmt.Errorf("update node capability instance: %w", err)
	}
	return nil
}

func validateSnapshotAdvance(previous, next *capabilityv1.CapabilitySnapshot) (bool, error) {
	if previous == nil {
		return false, nil
	}
	if previous.GetNodeInstanceID() != next.GetNodeInstanceID() {
		if previous.GetSnapshotID() == next.GetSnapshotID() {
			return false, fmt.Errorf("new node instance must publish a new capability snapshot identity")
		}
		return false, nil
	}
	if next.GetSequence() > previous.GetSequence() {
		if previous.GetSnapshotID() == next.GetSnapshotID() {
			return false, fmt.Errorf("capability snapshot identity must change when sequence advances")
		}
		if next.GetCollectedAt().AsTime().Before(previous.GetCollectedAt().AsTime()) {
			return false, fmt.Errorf("capability snapshot collected_at must not move backwards within node instance %q", next.GetNodeInstanceID())
		}
		if !sameObservationKeys(previous, next) {
			return false, fmt.Errorf("capability observation ownership cannot change within node instance %q", next.GetNodeInstanceID())
		}
		return false, nil
	}
	if next.GetSequence() == previous.GetSequence() && next.GetSnapshotID() == previous.GetSnapshotID() && proto.Equal(next, previous) {
		return true, nil
	}
	return false, fmt.Errorf("capability snapshot sequence must increase within node instance %q", next.GetNodeInstanceID())
}

func sameObservationKeys(left, right *capabilityv1.CapabilitySnapshot) bool {
	leftByKey, leftErr := observationsByKey(left)
	rightByKey, rightErr := observationsByKey(right)
	if leftErr != nil || rightErr != nil || len(leftByKey) != len(rightByKey) {
		return false
	}
	for key := range leftByKey {
		if _, exists := rightByKey[key]; !exists {
			return false
		}
	}
	return true
}

func capabilityTransitions(previous, next *capabilityv1.CapabilitySnapshot, now time.Time) ([]capabilityTransition, error) {
	oldByKey, err := observationsByKey(previous)
	if err != nil {
		return nil, err
	}
	newByKey, err := observationsByKey(next)
	if err != nil {
		return nil, err
	}
	keys := make(map[string]struct{}, len(oldByKey)+len(newByKey))
	for key := range oldByKey {
		keys[key] = struct{}{}
	}
	for key := range newByKey {
		keys[key] = struct{}{}
	}
	ids := make([]string, 0, len(keys))
	for key := range keys {
		ids = append(ids, key)
	}
	sort.Strings(ids)
	result := make([]capabilityTransition, 0, len(ids))
	for _, id := range ids {
		oldObservation := oldByKey[id]
		newObservation := newByKey[id]
		// A transition is historical evidence. Evaluate each side at the time
		// its own snapshot was published; evaluating the old side at the new
		// report time would retroactively turn an expired AVAILABLE observation
		// into UNKNOWN and destroy the actual state change.
		evaluation, changed := nodecapability.EvaluateObservationTransition(
			previous, oldObservation, snapshotEvaluationTime(previous, now),
			next, newObservation, snapshotEvaluationTime(next, now),
		)
		if !changed {
			continue
		}
		key := newObservation.GetKey()
		if key == nil {
			key = oldObservation.GetKey()
		}
		observedAt := snapshotEvaluationTime(next, now).UTC()
		if newObservation != nil && newObservation.GetObservedAt() != nil {
			observedAt = newObservation.GetObservedAt().AsTime().UTC()
		}
		result = append(result, capabilityTransition{
			snapshotSequence: next.GetSequence(),
			key:              key,
			keyID:            id,
			oldState:         evaluation.Previous.State,
			newState:         evaluation.Current.State,
			oldEvidence:      cloneEvidence(oldObservation.GetEvidence()),
			newEvidence:      cloneEvidence(newObservation.GetEvidence()),
			oldReasonCode:    evaluation.Previous.ReasonCode,
			newReasonCode:    evaluation.Current.ReasonCode,
			reason:           evaluation.Current.Reason,
			observedAt:       observedAt,
		})
	}
	return result, nil
}

func snapshotEvaluationTime(snapshot *capabilityv1.CapabilitySnapshot, fallback time.Time) time.Time {
	if snapshot != nil && snapshot.GetCollectedAt() != nil {
		return snapshot.GetCollectedAt().AsTime()
	}
	return fallback
}

func observationsByKey(snapshot *capabilityv1.CapabilitySnapshot) (map[string]*capabilityv1.CapabilityObservation, error) {
	result := make(map[string]*capabilityv1.CapabilityObservation)
	for _, observation := range snapshot.GetObservations() {
		id, err := nodecapability.KeyID(observation.GetKey())
		if err != nil {
			return nil, err
		}
		if _, duplicate := result[id]; duplicate {
			return nil, fmt.Errorf("duplicate capability observation %q", id)
		}
		result[id] = observation
	}
	return result, nil
}

func enqueueAffectedAllocations(ctx context.Context, tx pgx.Tx, nodeID string, transitions []capabilityTransition, now time.Time) error {
	changed := make(map[string]int64, len(transitions))
	for _, transition := range transitions {
		changed[transition.keyID] = max(changed[transition.keyID], transition.snapshotSequence)
	}
	keyIDs := make([]string, 0, len(changed))
	for keyID := range changed {
		keyIDs = append(keyIDs, keyID)
	}
	rows, err := tx.Query(ctx, `
		SELECT d.allocation_id, d.capability_key_id
		FROM allocation_capability_dependencies d
		JOIN allocations a ON a.allocation_id = d.allocation_id
		WHERE d.node_id = $1 AND d.capability_key_id = ANY($2::text[])
		  AND a.status IN ($3, $4, $5, $6)
		  AND d.loss_policy <> $7
		ORDER BY d.allocation_id, d.capability_key_id
		FOR UPDATE OF d
	`, nodeID, keyIDs,
		commonv1.AllocationStatus_ALLOCATION_STATUS_RESERVED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_BOUND.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING.String(),
		capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY.String())
	if err != nil {
		return fmt.Errorf("load allocations affected by capability transitions: %w", err)
	}
	defer rows.Close()
	affected := make(map[string][]string)
	for rows.Next() {
		var allocationID, keyID string
		if err := rows.Scan(&allocationID, &keyID); err != nil {
			return fmt.Errorf("scan capability-dependent allocation: %w", err)
		}
		affected[allocationID] = append(affected[allocationID], keyID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate capability-dependent allocations: %w", err)
	}
	for allocationID, pendingKeys := range affected {
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocation_capability_reconcile_queue (
				allocation_id, next_run_at, created_at, updated_at
			) VALUES ($1, $2, $2, $2)
			ON CONFLICT (allocation_id) DO UPDATE SET
				next_run_at = LEAST(allocation_capability_reconcile_queue.next_run_at, EXCLUDED.next_run_at),
				updated_at = EXCLUDED.updated_at
		`, allocationID, now.UTC()); err != nil {
			return fmt.Errorf("enqueue allocation %q capability reconcile: %w", allocationID, err)
		}
		for _, keyID := range pendingKeys {
			if _, err := tx.Exec(ctx, `
				INSERT INTO allocation_capability_reconcile_pending_keys (
					allocation_id, capability_key_id, snapshot_sequence, created_at, updated_at
				) VALUES ($1, $2, $3, $4, $4)
				ON CONFLICT (allocation_id, capability_key_id) DO UPDATE SET
					snapshot_sequence = GREATEST(allocation_capability_reconcile_pending_keys.snapshot_sequence, EXCLUDED.snapshot_sequence),
					updated_at = EXCLUDED.updated_at
			`, allocationID, keyID, changed[keyID], now.UTC()); err != nil {
				return fmt.Errorf("enqueue allocation %q capability key %q: %w", allocationID, keyID, err)
			}
		}
	}
	return nil
}

func cloneEvidence(in *capabilityv1.CapabilityEvidence) *capabilityv1.CapabilityEvidence {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*capabilityv1.CapabilityEvidence)
}
