package pgallocation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type capabilityExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func InsertCapabilityDependencies(ctx context.Context, executor capabilityExecutor, allocationID, nodeID string, dependencies []*capabilityv1.CapabilityDependency, now time.Time) error {
	if len(dependencies) == 0 {
		return nil
	}
	if err := capabilitycontract.ValidateDependencySet(dependencies, now); err != nil {
		return fmt.Errorf("validate placement capability dependencies: %w", err)
	}
	for _, dependency := range dependencies {
		keyID, _ := capabilitycontract.KeyID(dependency.GetKey())
		keyJSON, err := protojson.Marshal(dependency.GetKey())
		if err != nil {
			return err
		}
		dependencyJSON, err := protojson.Marshal(dependency)
		if err != nil {
			return err
		}
		if _, err := executor.Exec(ctx, `
			INSERT INTO allocation_capability_dependencies (
				allocation_id, node_id, capability_key_id, capability_key, loss_policy,
				placement_dependency, admitted_dependency, created_at, updated_at
			) VALUES ($1, $2, $3, $4::jsonb, $5, $6::jsonb, NULL, $7, $7)
		`, strings.TrimSpace(allocationID), strings.TrimSpace(nodeID), keyID, string(keyJSON), dependency.GetLossPolicy().String(), string(dependencyJSON), now.UTC()); err != nil {
			return fmt.Errorf("insert allocation capability dependency %q: %w", keyID, err)
		}
	}
	return nil
}

func LoadCapabilityDependencies(ctx context.Context, executor capabilityExecutor, allocationID string) ([]*capabilityv1.CapabilityDependency, error) {
	rows, err := executor.Query(ctx, `
		SELECT COALESCE(admitted_dependency, placement_dependency)
		FROM allocation_capability_dependencies
		WHERE allocation_id = $1
		ORDER BY capability_key_id
	`, strings.TrimSpace(allocationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var dependencies []*capabilityv1.CapabilityDependency
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		dependency := &capabilityv1.CapabilityDependency{}
		if err := protojson.Unmarshal(payload, dependency); err != nil {
			return nil, fmt.Errorf("unmarshal allocation capability dependency: %w", err)
		}
		dependencies = append(dependencies, dependency)
	}
	return dependencies, rows.Err()
}

// RecordCapabilityAdmission exclusively binds the immutable create-time proof
// set and atomically projects the matching conditions without mutating
// allocation lifecycle state. Once the admission header exists, an exact RPC
// replay is idempotent and can never replace the admitted proofs or rewind a
// newer condition projection.
func RecordCapabilityAdmission(ctx context.Context, executor capabilityExecutor, allocationID string, admission *allocationkernel.CapabilityAdmission, now time.Time) error {
	if admission == nil {
		return fmt.Errorf("allocation capability admission is required")
	}
	if admission.Attempt <= 0 {
		return fmt.Errorf("allocation capability admission requires a positive attempt")
	}
	if admission.ConditionSet == nil {
		return fmt.Errorf("allocation capability admission requires a full condition set")
	}
	if admission.ConditionSet.GetRevision() <= 0 || admission.ConditionSet.GetObservedAt() == nil {
		return fmt.Errorf("allocation capability condition revision and observed_at are required")
	}
	if err := capabilitycontract.ValidateDependencySet(admission.Dependencies, now); err != nil {
		return fmt.Errorf("validate admitted capability dependencies: %w", err)
	}
	if err := capabilitycontract.ValidateConditionSet(admission.ConditionSet, now); err != nil {
		return fmt.Errorf("validate admitted capability conditions: %w", err)
	}
	// A replay is still a create-admission assertion. Validate the healthy
	// proof binding before consulting the immutable header so a stale or buggy
	// node cannot present the current degraded condition projection as the
	// historical proof that allowed workload activation.
	if err := validateCreateAdmissionConditions(admission.Dependencies, admission.ConditionSet); err != nil {
		return err
	}
	dependencyDigest, err := capabilityDependencySetDigest(admission.Dependencies)
	if err != nil {
		return err
	}
	var currentAttempt int64
	if err := executor.QueryRow(ctx, `SELECT attempt FROM allocations WHERE allocation_id = $1 FOR UPDATE`, strings.TrimSpace(allocationID)).Scan(&currentAttempt); err != nil {
		return fmt.Errorf("lock allocation capability verification: %w", err)
	}
	if currentAttempt != admission.Attempt {
		return fmt.Errorf("allocation capability admission attempt %d does not match current attempt %d", admission.Attempt, currentAttempt)
	}
	var admittedAttempt int64
	var admittedDigest string
	err = executor.QueryRow(ctx, `
		SELECT allocation_attempt, dependency_set_digest
		FROM allocation_capability_admissions
		WHERE allocation_id = $1
	`, strings.TrimSpace(allocationID)).Scan(&admittedAttempt, &admittedDigest)
	switch {
	case err == nil:
		if admittedAttempt != admission.Attempt || admittedDigest != dependencyDigest {
			return fmt.Errorf("create capability admission conflicts with immutable proof for attempt %d", admittedAttempt)
		}
		existing, loadErr := LoadCapabilityDependencies(ctx, executor, allocationID)
		if loadErr != nil {
			return loadErr
		}
		if !sameDependencyProofs(existing, admission.Dependencies) {
			return fmt.Errorf("create capability admission digest matched but admitted proofs differ")
		}
		return nil
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("read immutable capability admission: %w", err)
	}
	existing, err := LoadCapabilityDependencies(ctx, executor, allocationID)
	if err != nil {
		return err
	}
	if !sameDependencyKeys(existing, admission.Dependencies) {
		return fmt.Errorf("admitted capability dependency keys do not match placement requirements")
	}
	for _, dependency := range admission.Dependencies {
		keyID, _ := capabilitycontract.KeyID(dependency.GetKey())
		payload, err := protojson.Marshal(dependency)
		if err != nil {
			return err
		}
		tag, err := executor.Exec(ctx, `
			UPDATE allocation_capability_dependencies
			SET admitted_dependency = $3::jsonb, updated_at = $4
			WHERE allocation_id = $1 AND capability_key_id = $2
		`, strings.TrimSpace(allocationID), keyID, string(payload), now.UTC())
		if err != nil || tag.RowsAffected() != 1 {
			return fmt.Errorf("persist admitted capability proof %q: affected=%d: %w", keyID, tag.RowsAffected(), err)
		}
	}
	if err := ReplaceCapabilityConditions(ctx, executor, allocationID, admission.ConditionSet, now); err != nil {
		return err
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO allocation_capability_admissions (
			allocation_id, allocation_attempt, dependency_set_digest, admitted_at
		) VALUES ($1, $2, $3, $4)
	`, strings.TrimSpace(allocationID), admission.Attempt, dependencyDigest, now.UTC()); err != nil {
		return fmt.Errorf("persist immutable capability admission: %w", err)
	}
	return nil
}

func validateCreateAdmissionConditions(dependencies []*capabilityv1.CapabilityDependency, set *capabilityv1.CapabilityConditionSet) error {
	if set == nil || len(set.GetConditions()) != len(dependencies) {
		return fmt.Errorf("create capability conditions do not exactly match admitted dependencies")
	}
	byKey := make(map[string]*capabilityv1.CapabilityDependency, len(dependencies))
	for _, dependency := range dependencies {
		keyID, err := capabilitycontract.KeyID(dependency.GetKey())
		if err != nil {
			return err
		}
		byKey[keyID] = dependency
	}
	for _, condition := range set.GetConditions() {
		keyID, err := capabilitycontract.KeyID(condition.GetKey())
		if err != nil {
			return err
		}
		dependency := byKey[keyID]
		if dependency == nil {
			return fmt.Errorf("create capability condition %q is not an admitted dependency", keyID)
		}
		if condition.GetState() != capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY ||
			condition.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
			return fmt.Errorf("create capability condition %q must be healthy and available", keyID)
		}
		if !proto.Equal(condition.GetProof(), dependency.GetSelectedObservation()) {
			return fmt.Errorf("create capability condition %q does not bind its admitted observation proof", keyID)
		}
		if err := capabilitycontract.ValidateObservationProof(condition.GetProof(), condition.GetObservedAt().AsTime()); err != nil {
			return fmt.Errorf("create capability condition %q proof was not current when admitted: %w", keyID, err)
		}
		delete(byKey, keyID)
	}
	if len(byKey) != 0 {
		return fmt.Errorf("create capability condition set omits admitted dependencies")
	}
	return nil
}

func sameDependencyProofs(left, right []*capabilityv1.CapabilityDependency) bool {
	if len(left) != len(right) {
		return false
	}
	canonical := func(items []*capabilityv1.CapabilityDependency) ([]*capabilityv1.CapabilityDependency, bool) {
		out := make([]*capabilityv1.CapabilityDependency, 0, len(items))
		seen := make(map[string]struct{}, len(items))
		for _, item := range items {
			keyID, err := capabilitycontract.KeyID(item.GetKey())
			if err != nil {
				return nil, false
			}
			if _, duplicate := seen[keyID]; duplicate {
				return nil, false
			}
			seen[keyID] = struct{}{}
			out = append(out, proto.Clone(item).(*capabilityv1.CapabilityDependency))
		}
		sort.Slice(out, func(i, j int) bool {
			leftID, _ := capabilitycontract.KeyID(out[i].GetKey())
			rightID, _ := capabilitycontract.KeyID(out[j].GetKey())
			return leftID < rightID
		})
		return out, true
	}
	leftCanonical, leftValid := canonical(left)
	rightCanonical, rightValid := canonical(right)
	if !leftValid || !rightValid {
		return false
	}
	for index := range leftCanonical {
		if !proto.Equal(leftCanonical[index], rightCanonical[index]) {
			return false
		}
	}
	return true
}

func capabilityDependencySetDigest(dependencies []*capabilityv1.CapabilityDependency) (string, error) {
	canonical := make([]*capabilityv1.CapabilityDependency, 0, len(dependencies))
	seen := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		keyID, err := capabilitycontract.KeyID(dependency.GetKey())
		if err != nil {
			return "", err
		}
		if _, duplicate := seen[keyID]; duplicate {
			return "", fmt.Errorf("duplicate capability dependency %q", keyID)
		}
		seen[keyID] = struct{}{}
		canonical = append(canonical, proto.Clone(dependency).(*capabilityv1.CapabilityDependency))
	}
	sort.Slice(canonical, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(canonical[i].GetKey())
		right, _ := capabilitycontract.KeyID(canonical[j].GetKey())
		return left < right
	})
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(&capabilityv1.CapabilityDependencySet{Dependencies: canonical})
	if err != nil {
		return "", fmt.Errorf("marshal capability dependency set: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func ReplaceCapabilityConditions(ctx context.Context, executor capabilityExecutor, allocationID string, set *capabilityv1.CapabilityConditionSet, now time.Time) error {
	canonicalSet, err := canonicalCapabilityConditionSet(set)
	if err != nil {
		return err
	}
	if err := capabilitycontract.ValidateConditionSet(canonicalSet, now); err != nil {
		return fmt.Errorf("validate capability condition set: %w", err)
	}
	payloadDigest, err := capabilityConditionSetDigest(canonicalSet)
	if err != nil {
		return err
	}
	var allocationAttempt int64
	if err := executor.QueryRow(ctx, `SELECT attempt FROM allocations WHERE allocation_id = $1 FOR UPDATE`, strings.TrimSpace(allocationID)).Scan(&allocationAttempt); err != nil {
		return fmt.Errorf("lock allocation capability condition projection: %w", err)
	}
	if err := validateStoredDependencyConditionKeys(ctx, executor, allocationID, canonicalSet.GetConditions()); err != nil {
		return err
	}
	var currentAttempt, currentRevision int64
	var currentDigest string
	if err := executor.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT allocation_attempt FROM allocation_capability_condition_sets WHERE allocation_id = $1), 0),
			COALESCE((SELECT revision FROM allocation_capability_condition_sets WHERE allocation_id = $1), 0),
			COALESCE((SELECT payload_digest FROM allocation_capability_condition_sets WHERE allocation_id = $1), '')
	`, strings.TrimSpace(allocationID)).Scan(&currentAttempt, &currentRevision, &currentDigest); err != nil {
		return fmt.Errorf("lock allocation capability conditions: %w", err)
	}
	replace, err := shouldReplaceCapabilityConditions(currentAttempt, currentRevision, allocationAttempt, canonicalSet.GetRevision())
	if err != nil {
		return err
	}
	if !replace {
		if currentAttempt == allocationAttempt && currentRevision == canonicalSet.GetRevision() && currentDigest != payloadDigest {
			return fmt.Errorf("capability condition revision %d for allocation attempt %d conflicts with its durable payload", currentRevision, currentAttempt)
		}
		return nil
	}
	// Children carry the parent revision as a composite foreign key. Remove
	// the previous full projection before advancing the set revision; the
	// surrounding transaction makes replacement atomic to readers.
	if _, err := executor.Exec(ctx, `DELETE FROM allocation_capability_conditions WHERE allocation_id = $1`, strings.TrimSpace(allocationID)); err != nil {
		return err
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO allocation_capability_condition_sets (allocation_id, allocation_attempt, revision, payload_digest, observed_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (allocation_id) DO UPDATE SET
			allocation_attempt = EXCLUDED.allocation_attempt, revision = EXCLUDED.revision,
			payload_digest = EXCLUDED.payload_digest, observed_at = EXCLUDED.observed_at, updated_at = EXCLUDED.updated_at
	`, strings.TrimSpace(allocationID), allocationAttempt, canonicalSet.GetRevision(), payloadDigest, canonicalSet.GetObservedAt().AsTime().UTC(), now.UTC()); err != nil {
		return fmt.Errorf("persist allocation capability condition set: %w", err)
	}
	for _, condition := range canonicalSet.GetConditions() {
		keyID, _ := capabilitycontract.KeyID(condition.GetKey())
		payload, err := protojson.Marshal(condition)
		if err != nil {
			return err
		}
		if _, err := executor.Exec(ctx, `
			INSERT INTO allocation_capability_conditions (
				allocation_id, capability_key_id, allocation_attempt, condition_revision, observed_at, condition, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		`, strings.TrimSpace(allocationID), keyID, allocationAttempt, canonicalSet.GetRevision(), canonicalSet.GetObservedAt().AsTime().UTC(), string(payload), now.UTC()); err != nil {
			return fmt.Errorf("insert allocation capability condition %q: %w", keyID, err)
		}
	}
	return nil
}

func canonicalCapabilityConditionSet(set *capabilityv1.CapabilityConditionSet) (*capabilityv1.CapabilityConditionSet, error) {
	if set == nil {
		return nil, fmt.Errorf("capability condition set is required")
	}
	canonical := proto.Clone(set).(*capabilityv1.CapabilityConditionSet)
	for _, condition := range canonical.GetConditions() {
		if _, err := capabilitycontract.KeyID(condition.GetKey()); err != nil {
			return nil, err
		}
	}
	sort.Slice(canonical.Conditions, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(canonical.Conditions[i].GetKey())
		right, _ := capabilitycontract.KeyID(canonical.Conditions[j].GetKey())
		return left < right
	})
	return canonical, nil
}

func capabilityConditionSetDigest(set *capabilityv1.CapabilityConditionSet) (string, error) {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(set)
	if err != nil {
		return "", fmt.Errorf("marshal capability condition payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateStoredDependencyConditionKeys(ctx context.Context, executor capabilityExecutor, allocationID string, conditions []*capabilityv1.CapabilityCondition) error {
	rows, err := executor.Query(ctx, `
		SELECT capability_key_id FROM allocation_capability_dependencies
		WHERE allocation_id = $1 ORDER BY capability_key_id
	`, strings.TrimSpace(allocationID))
	if err != nil {
		return fmt.Errorf("load allocation capability keys: %w", err)
	}
	defer rows.Close()
	stored := make(map[string]struct{})
	for rows.Next() {
		var keyID string
		if err := rows.Scan(&keyID); err != nil {
			return err
		}
		stored[keyID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(stored) != len(conditions) {
		return fmt.Errorf("capability condition keys do not exactly match allocation dependencies")
	}
	for _, condition := range conditions {
		keyID, err := capabilitycontract.KeyID(condition.GetKey())
		if err != nil {
			return err
		}
		if _, exists := stored[keyID]; !exists {
			return fmt.Errorf("capability condition key %q is not an allocation dependency", keyID)
		}
		delete(stored, keyID)
	}
	if len(stored) != 0 {
		return fmt.Errorf("capability condition set omits allocation dependencies")
	}
	return nil
}

func shouldReplaceCapabilityConditions(storedAttempt, storedRevision, allocationAttempt, incomingRevision int64) (bool, error) {
	if allocationAttempt <= 0 || incomingRevision <= 0 || storedAttempt < 0 || storedRevision < 0 {
		return false, fmt.Errorf("capability condition attempt and revision must be positive")
	}
	if storedAttempt > allocationAttempt {
		return false, fmt.Errorf("stored capability condition attempt %d is ahead of allocation attempt %d", storedAttempt, allocationAttempt)
	}
	if storedAttempt < allocationAttempt {
		return true, nil
	}
	return incomingRevision > storedRevision, nil
}

func sameDependencyKeys(left, right []*capabilityv1.CapabilityDependency) bool {
	if len(left) != len(right) {
		return false
	}
	ids := func(items []*capabilityv1.CapabilityDependency) ([]string, bool) {
		out := make([]string, 0, len(items))
		seen := make(map[string]struct{}, len(items))
		for _, item := range items {
			id, err := capabilitycontract.KeyID(item.GetKey())
			if err != nil {
				return nil, false
			}
			if _, duplicate := seen[id]; duplicate {
				return nil, false
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		sort.Strings(out)
		return out, true
	}
	leftIDs, leftValid := ids(left)
	rightIDs, rightValid := ids(right)
	if !leftValid || !rightValid {
		return false
	}
	for index := range leftIDs {
		if leftIDs[index] != rightIDs[index] {
			return false
		}
	}
	return true
}
