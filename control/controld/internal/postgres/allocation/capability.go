package pgallocation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
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

// InsertCapabilityRequirements persists the immutable Allocation specification.
// Node observations and admission-time copies never enter this table.
func InsertCapabilityRequirements(ctx context.Context, executor capabilityExecutor, allocationID string, requirements []*capabilityv1.CapabilityRequirement, now time.Time) error {
	if err := capabilitycontract.ValidateRequirements(requirements); err != nil {
		return fmt.Errorf("validate capability requirements: %w", err)
	}
	for _, requirement := range requirements {
		keyID, _ := capabilitycontract.KeyID(requirement.GetKey())
		keyJSON, err := protojson.Marshal(requirement.GetKey())
		if err != nil {
			return err
		}
		if _, err := executor.Exec(ctx, `
			INSERT INTO allocation_capability_requirements (
				allocation_id, capability_key_id, capability_key, loss_policy, created_at
			) VALUES ($1, $2, $3::jsonb, $4, $5)
		`, strings.TrimSpace(allocationID), keyID, string(keyJSON), requirement.GetLossPolicy().String(), now.UTC()); err != nil {
			return fmt.Errorf("insert allocation capability requirement %q: %w", keyID, err)
		}
	}
	return nil
}

func LoadCapabilityRequirements(ctx context.Context, executor capabilityExecutor, allocationID string) ([]*capabilityv1.CapabilityRequirement, error) {
	rows, err := executor.Query(ctx, `
		SELECT capability_key, loss_policy
		FROM allocation_capability_requirements
		WHERE allocation_id = $1 ORDER BY capability_key_id
	`, strings.TrimSpace(allocationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requirements []*capabilityv1.CapabilityRequirement
	for rows.Next() {
		var payload []byte
		var policyName string
		if err := rows.Scan(&payload, &policyName); err != nil {
			return nil, err
		}
		key := &capabilityv1.CapabilityKey{}
		if err := protojson.Unmarshal(payload, key); err != nil {
			return nil, fmt.Errorf("unmarshal allocation capability key: %w", err)
		}
		policyNumber, ok := capabilityv1.CapabilityLossPolicy_value[policyName]
		if !ok {
			return nil, fmt.Errorf("unknown capability loss policy %q", policyName)
		}
		requirements = append(requirements, &capabilityv1.CapabilityRequirement{Key: key, LossPolicy: capabilityv1.CapabilityLossPolicy(policyNumber)})
	}
	return requirements, rows.Err()
}

// ReplaceCapabilityConditions stores one ordered diagnostic projection. A
// newer observed_at replaces the current value; exact replays are idempotent
// and conflicting equal-time payloads fail closed.
func ReplaceCapabilityConditions(ctx context.Context, executor pgx.Tx, allocationID string, set *capabilityv1.CapabilityConditionSet, now time.Time) error {
	canonical, err := canonicalCapabilityConditionSet(set)
	if err != nil {
		return err
	}
	if err := capabilitycontract.ValidateConditionSet(canonical, now); err != nil {
		return fmt.Errorf("validate capability condition set: %w", err)
	}
	var lockedID, lifecycleState string
	if err := executor.QueryRow(ctx, `SELECT allocation_id, lifecycle_state FROM allocations WHERE allocation_id = $1 FOR UPDATE`, strings.TrimSpace(allocationID)).Scan(&lockedID, &lifecycleState); err != nil {
		return fmt.Errorf("lock allocation capability conditions: %w", err)
	}
	switch lifecycleState {
	case commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_STOPPED.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASING.String(),
		commonv1.AllocationLifecycleState_ALLOCATION_LIFECYCLE_STATE_RELEASED.String():
		return nil
	}
	if err := validateStoredRequirementConditionKeys(ctx, executor, allocationID, canonical.GetConditions()); err != nil {
		return err
	}
	payload, err := protojson.Marshal(canonical)
	if err != nil {
		return fmt.Errorf("marshal capability conditions: %w", err)
	}
	var existingPayload []byte
	var existingObservedAt time.Time
	err = executor.QueryRow(ctx, `SELECT conditions, observed_at FROM allocation_capability_conditions WHERE allocation_id = $1`, strings.TrimSpace(allocationID)).Scan(&existingPayload, &existingObservedAt)
	if err == nil {
		incomingAt := canonical.GetObservedAt().AsTime().UTC()
		if incomingAt.Before(existingObservedAt) {
			return nil
		}
		if incomingAt.Equal(existingObservedAt) {
			existing := &capabilityv1.CapabilityConditionSet{}
			if err := protojson.Unmarshal(existingPayload, existing); err != nil {
				return fmt.Errorf("unmarshal stored capability conditions: %w", err)
			}
			if !proto.Equal(existing, canonical) {
				return fmt.Errorf("capability conditions conflict at observed_at %s", incomingAt.Format(time.RFC3339Nano))
			}
			return nil
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("load capability conditions: %w", err)
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO allocation_capability_conditions (allocation_id, observed_at, conditions)
		VALUES ($1, $2, $3::jsonb)
		ON CONFLICT (allocation_id) DO UPDATE SET observed_at = EXCLUDED.observed_at, conditions = EXCLUDED.conditions
	`, strings.TrimSpace(allocationID), canonical.GetObservedAt().AsTime().UTC(), string(payload)); err != nil {
		return fmt.Errorf("persist capability conditions: %w", err)
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

func validateStoredRequirementConditionKeys(ctx context.Context, executor capabilityExecutor, allocationID string, conditions []*capabilityv1.CapabilityCondition) error {
	rows, err := executor.Query(ctx, `SELECT capability_key_id FROM allocation_capability_requirements WHERE allocation_id = $1`, strings.TrimSpace(allocationID))
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
		return fmt.Errorf("capability condition keys do not exactly match allocation requirements")
	}
	for _, condition := range conditions {
		keyID, err := capabilitycontract.KeyID(condition.GetKey())
		if err != nil {
			return err
		}
		if _, ok := stored[keyID]; !ok {
			return fmt.Errorf("capability condition key %q is not an allocation requirement", keyID)
		}
		delete(stored, keyID)
	}
	if len(stored) != 0 {
		return fmt.Errorf("capability condition set omits allocation requirements")
	}
	return nil
}
