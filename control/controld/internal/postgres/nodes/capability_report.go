package pgnodes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

type capabilityTransition struct {
	key        *capabilityv1.CapabilityKey
	oldState   capabilityv1.CapabilityState
	newState   capabilityv1.CapabilityState
	reasonCode capabilityv1.CapabilityReasonCode
}

func persistCapabilityReport(ctx context.Context, tx pgx.Tx, nodeID string, previous, next *nodev1.NodeSummary, reportedAt time.Time) ([]capabilityTransition, error) {
	nextSnapshot := next.GetCapabilitySnapshot()
	if err := nodecapability.ValidateSnapshot(nextSnapshot, reportedAt); err != nil {
		return nil, fmt.Errorf("validate capability snapshot: %w", err)
	}
	previousSnapshot := previous.GetCapabilitySnapshot()
	if err := persistCapabilityInstance(ctx, tx, nodeID, previousSnapshot, nextSnapshot); err != nil {
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
	return transitions, nil
}

func persistCapabilityInstance(ctx context.Context, tx pgx.Tx, nodeID string, previous, next *capabilityv1.CapabilitySnapshot) error {
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
				node_id, node_instance_id, last_sequence
			) VALUES ($1, $2, $3)
		`, nodeID, next.GetNodeInstanceID(), next.GetSequence()); err != nil {
			return fmt.Errorf("insert node capability instance: %w", err)
		}
		return nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE node_capability_instances
		SET last_sequence = $3
		WHERE node_id = $1 AND node_instance_id = $2
	`, nodeID, next.GetNodeInstanceID(), next.GetSequence()); err != nil {
		return fmt.Errorf("update node capability instance: %w", err)
	}
	return nil
}

func validateSnapshotAdvance(previous, next *capabilityv1.CapabilitySnapshot) (bool, error) {
	if previous == nil {
		return false, nil
	}
	if previous.GetNodeInstanceID() != next.GetNodeInstanceID() {
		return false, nil
	}
	if next.GetSequence() > previous.GetSequence() {
		if next.GetCollectedAt().AsTime().Before(previous.GetCollectedAt().AsTime()) {
			return false, fmt.Errorf("capability snapshot collected_at must not move backwards within node instance %q", next.GetNodeInstanceID())
		}
		if !sameObservationKeys(previous, next) {
			return false, fmt.Errorf("capability observation ownership cannot change within node instance %q", next.GetNodeInstanceID())
		}
		return false, nil
	}
	if next.GetSequence() == previous.GetSequence() && proto.Equal(next, previous) {
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
		// Compare each side at the time its own snapshot was published. Using
		// the new report time for the old side would turn an expired AVAILABLE
		// observation into UNKNOWN and destroy the actual state change.
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
		result = append(result, capabilityTransition{
			key:        key,
			oldState:   evaluation.Previous.State,
			newState:   evaluation.Current.State,
			reasonCode: evaluation.Current.ReasonCode,
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
