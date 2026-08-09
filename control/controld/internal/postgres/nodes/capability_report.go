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
	key           *capabilityv1.CapabilityKey
	keyID         string
	oldState      capabilityv1.CapabilityState
	newState      capabilityv1.CapabilityState
	oldEvidenceID string
	newEvidenceID string
	reasonCode    capabilityv1.CapabilityReasonCode
	reason        string
	observedAt    time.Time
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
		if _, err := tx.Exec(ctx, `
			INSERT INTO node_capability_transitions (
				transition_id, node_id, snapshot_id, snapshot_sequence, capability_key, capability_key_id,
				old_state, new_state, old_evidence_id, new_evidence_id, reason_code, reason, observed_at, reported_at
			) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (node_id, snapshot_id, capability_key_id) DO NOTHING
		`, transitionID, nodeID, nextSnapshot.GetSnapshotID(), nextSnapshot.GetSequence(), string(keyJSON), transition.keyID,
			transition.oldState.String(), transition.newState.String(), transition.oldEvidenceID, transition.newEvidenceID,
			transition.reasonCode.String(), transition.reason, transition.observedAt, reportedAt.UTC()); err != nil {
			return nil, fmt.Errorf("insert capability transition %q: %w", transition.keyID, err)
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
		return false, nil
	}
	if next.GetSequence() == previous.GetSequence() && next.GetSnapshotID() == previous.GetSnapshotID() && proto.Equal(next, previous) {
		return true, nil
	}
	return false, fmt.Errorf("capability snapshot sequence must increase within node instance %q", next.GetNodeInstanceID())
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
		oldState := effectiveState(previous, oldObservation, now)
		newState := effectiveState(next, newObservation, now)
		oldEvidence := oldObservation.GetEvidence().GetEvidenceID()
		newEvidence := newObservation.GetEvidence().GetEvidenceID()
		if oldState == newState && oldEvidence == newEvidence {
			continue
		}
		key := newObservation.GetKey()
		if key == nil {
			key = oldObservation.GetKey()
		}
		observedAt := now.UTC()
		if newObservation.GetObservedAt() != nil {
			observedAt = newObservation.GetObservedAt().AsTime().UTC()
		}
		result = append(result, capabilityTransition{
			key:           key,
			keyID:         id,
			oldState:      oldState,
			newState:      newState,
			oldEvidenceID: oldEvidence,
			newEvidenceID: newEvidence,
			reasonCode:    newObservation.GetReasonCode(),
			reason:        newObservation.GetReason(),
			observedAt:    observedAt,
		})
	}
	return result, nil
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

func effectiveState(snapshot *capabilityv1.CapabilitySnapshot, observation *capabilityv1.CapabilityObservation, now time.Time) capabilityv1.CapabilityState {
	if observation == nil {
		return capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
	}
	if _, available := nodecapability.AvailableObservation(snapshot, observation.GetKey(), now); available {
		return capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	}
	if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		return capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
	}
	return observation.GetState()
}

func enqueueAffectedAllocations(ctx context.Context, tx pgx.Tx, nodeID string, transitions []capabilityTransition, now time.Time) error {
	changed := make(map[string]struct{}, len(transitions))
	for _, transition := range transitions {
		changed[transition.keyID] = struct{}{}
	}
	rows, err := tx.Query(ctx, `
		SELECT allocation_id, capability_dependencies
		FROM allocations
		WHERE node_id = $1 AND status NOT IN ($2, $3, $4)
		FOR UPDATE
	`, nodeID,
		commonv1.AllocationStatus_ALLOCATION_STATUS_EXITED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_FAILED.String(),
		commonv1.AllocationStatus_ALLOCATION_STATUS_RELEASED.String())
	if err != nil {
		return fmt.Errorf("load allocations affected by capability transitions: %w", err)
	}
	defer rows.Close()
	type affectedAllocation struct {
		id           string
		dependencies []*capabilityv1.CapabilityDependency
	}
	var affected []affectedAllocation
	for rows.Next() {
		var allocationID string
		var dependenciesJSON []byte
		if err := rows.Scan(&allocationID, &dependenciesJSON); err != nil {
			return fmt.Errorf("scan capability-dependent allocation: %w", err)
		}
		set := &capabilityv1.CapabilityDependencySet{}
		if len(dependenciesJSON) > 0 {
			if err := protojson.Unmarshal(dependenciesJSON, set); err != nil {
				return fmt.Errorf("unmarshal allocation %q capability dependencies: %w", allocationID, err)
			}
		}
		pending := make([]*capabilityv1.CapabilityDependency, 0, len(set.GetDependencies()))
		for _, dependency := range set.GetDependencies() {
			id, err := nodecapability.KeyID(dependency.GetKey())
			if err != nil {
				return err
			}
			if _, needsReconcile := changed[id]; needsReconcile {
				pending = append(pending, dependency)
			}
		}
		if len(pending) > 0 {
			affected = append(affected, affectedAllocation{id: allocationID, dependencies: pending})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate capability-dependent allocations: %w", err)
	}
	for _, allocation := range affected {
		merged, err := mergeQueuedDependencies(ctx, tx, allocation.id, allocation.dependencies)
		if err != nil {
			return err
		}
		payload, err := protojson.Marshal(&capabilityv1.CapabilityDependencySet{Dependencies: merged})
		if err != nil {
			return fmt.Errorf("marshal capability reconcile dependencies: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocation_capability_reconcile_queue (
				allocation_id, pending_dependencies, next_run_at, created_at, updated_at
			) VALUES ($1, $2::jsonb, $3, $3, $3)
			ON CONFLICT (allocation_id) DO UPDATE SET
				pending_dependencies = EXCLUDED.pending_dependencies,
				next_run_at = LEAST(allocation_capability_reconcile_queue.next_run_at, EXCLUDED.next_run_at),
				lease_owner = '', lease_expires_at = NULL, updated_at = EXCLUDED.updated_at
		`, allocation.id, string(payload), now.UTC()); err != nil {
			return fmt.Errorf("enqueue allocation %q capability reconcile: %w", allocation.id, err)
		}
	}
	return nil
}

func mergeQueuedDependencies(ctx context.Context, tx pgx.Tx, allocationID string, additions []*capabilityv1.CapabilityDependency) ([]*capabilityv1.CapabilityDependency, error) {
	set := &capabilityv1.CapabilityDependencySet{Dependencies: append([]*capabilityv1.CapabilityDependency(nil), additions...)}
	var existingJSON []byte
	err := tx.QueryRow(ctx, `SELECT pending_dependencies FROM allocation_capability_reconcile_queue WHERE allocation_id = $1 FOR UPDATE`, allocationID).Scan(&existingJSON)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lock allocation %q capability reconcile: %w", allocationID, err)
	}
	if err == nil {
		existing := &capabilityv1.CapabilityDependencySet{}
		if unmarshalErr := protojson.Unmarshal(existingJSON, existing); unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshal queued dependencies for allocation %q: %w", allocationID, unmarshalErr)
		}
		set.Dependencies = append(set.Dependencies, existing.GetDependencies()...)
	}
	byKey := make(map[string]*capabilityv1.CapabilityDependency, len(set.GetDependencies()))
	for _, dependency := range set.GetDependencies() {
		id, keyErr := nodecapability.KeyID(dependency.GetKey())
		if keyErr != nil {
			return nil, keyErr
		}
		byKey[id] = dependency
	}
	ids := make([]string, 0, len(byKey))
	for id := range byKey {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]*capabilityv1.CapabilityDependency, 0, len(ids))
	for _, id := range ids {
		result = append(result, byKey[id])
	}
	return result, nil
}
