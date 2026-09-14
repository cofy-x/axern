package nodecapability

import (
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

// ResolveRequirements verifies the latest ordered Node observation and returns
// only the immutable requirements that travel with an Allocation.
func ResolveRequirements(snapshot *capabilityv1.CapabilitySnapshot, keys []*capabilityv1.CapabilityKey, now time.Time) ([]*capabilityv1.CapabilityRequirement, error) {
	if err := ValidateRequirementKeys(keys); err != nil {
		return nil, err
	}
	if err := ValidateSnapshot(snapshot, now); err != nil {
		return nil, fmt.Errorf("invalid capability snapshot: %w", err)
	}
	resolved := make([]*capabilityv1.CapabilityRequirement, 0, len(keys))
	for _, key := range keys {
		id, _ := KeyID(key)
		if _, ok := AvailableObservation(snapshot, key, now); !ok {
			return nil, fmt.Errorf("required capability %q is not available", id)
		}
		policy, err := LossPolicy(key)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, &capabilityv1.CapabilityRequirement{Key: CloneKey(key), LossPolicy: policy})
	}
	sort.Slice(resolved, func(i, j int) bool {
		left, _ := KeyID(resolved[i].GetKey())
		right, _ := KeyID(resolved[j].GetKey())
		return left < right
	})
	return resolved, nil
}

func ValidateRequirementKeys(keys []*capabilityv1.CapabilityKey) error {
	if len(keys) > MaxExtensionCapabilities+16 {
		return fmt.Errorf("capability requirement count is too large")
	}
	seen := make(map[string]struct{}, len(keys))
	extensionNames := make(map[string]struct{})
	extensions := 0
	for _, key := range keys {
		id, err := KeyID(key)
		if err != nil {
			return err
		}
		if !IsWorkloadRequirement(key) {
			return fmt.Errorf("internal capability %q cannot be a workload requirement", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate capability requirement %q", id)
		}
		seen[id] = struct{}{}
		if key.GetExtension() != nil {
			extensions++
			name := NormalizeExtension(key.GetExtension()).GetName()
			if _, duplicate := extensionNames[name]; duplicate {
				return fmt.Errorf("duplicate extension capability name %q", name)
			}
			extensionNames[name] = struct{}{}
		}
	}
	if extensions > MaxExtensionCapabilities {
		return fmt.Errorf("extension capability count exceeds %d", MaxExtensionCapabilities)
	}
	return nil
}

func ValidateRequirements(requirements []*capabilityv1.CapabilityRequirement) error {
	keys := make([]*capabilityv1.CapabilityKey, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement == nil {
			return fmt.Errorf("nil capability requirement")
		}
		keys = append(keys, requirement.GetKey())
	}
	if err := ValidateRequirementKeys(keys); err != nil {
		return err
	}
	for _, requirement := range requirements {
		id, _ := KeyID(requirement.GetKey())
		policy, err := LossPolicy(requirement.GetKey())
		if err != nil || requirement.GetLossPolicy() != policy {
			return fmt.Errorf("capability requirement %q has invalid loss policy", id)
		}
	}
	return nil
}

func AvailableObservation(snapshot *capabilityv1.CapabilitySnapshot, key *capabilityv1.CapabilityKey, now time.Time) (*capabilityv1.CapabilityObservation, bool) {
	if snapshot == nil || key == nil || snapshot.GetCollectedAt() == nil || snapshot.GetCollectedAt().AsTime().After(now.Add(time.Minute)) {
		return nil, false
	}
	want, err := KeyID(key)
	if err != nil {
		return nil, false
	}
	byKey, err := observationsByKey(snapshot.GetObservations())
	if err != nil || !availableObservationSet(byKey, want, now, make(map[string]bool)) {
		return nil, false
	}
	return byKey[want], true
}

func observationsByKey(observations []*capabilityv1.CapabilityObservation) (map[string]*capabilityv1.CapabilityObservation, error) {
	byKey := make(map[string]*capabilityv1.CapabilityObservation, len(observations))
	extensionNames := make(map[string]struct{})
	for _, observation := range observations {
		if observation == nil {
			return nil, fmt.Errorf("nil capability observation")
		}
		id, err := KeyID(observation.GetKey())
		if err != nil {
			return nil, err
		}
		if _, duplicate := byKey[id]; duplicate {
			return nil, fmt.Errorf("duplicate capability observation %q", id)
		}
		if extension := observation.GetKey().GetExtension(); extension != nil {
			name := NormalizeExtension(extension).GetName()
			if _, duplicate := extensionNames[name]; duplicate {
				return nil, fmt.Errorf("duplicate extension capability observation name %q", name)
			}
			extensionNames[name] = struct{}{}
		}
		byKey[id] = observation
	}
	return byKey, nil
}

func availableObservationSet(byKey map[string]*capabilityv1.CapabilityObservation, id string, now time.Time, visiting map[string]bool) bool {
	if visiting[id] {
		return false
	}
	observation := byKey[id]
	if !observationValid(observation, now) {
		return false
	}
	if observation.GetKey().GetExtension() != nil {
		return true
	}
	dependencies, err := PlatformDependencyKeys(observation.GetKey().GetPlatform())
	if err != nil {
		return false
	}
	visiting[id] = true
	defer delete(visiting, id)
	for _, dependency := range dependencies {
		dependencyID, err := KeyID(dependency)
		if err != nil || !availableObservationSet(byKey, dependencyID, now, visiting) {
			return false
		}
	}
	return true
}

func observationValid(observation *capabilityv1.CapabilityObservation, now time.Time) bool {
	if observation == nil || observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || observation.GetObservedAt() == nil || observation.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
		return false
	}
	provider, identity, err := ObservationOwner(observation.GetKey())
	if err != nil || observation.GetProvider() != provider || observation.GetObservedAt().AsTime().After(now.Add(time.Minute)) {
		return false
	}
	if err := ValidateEvidence(observation.GetEvidence(), identity); err != nil {
		return false
	}
	if err := validateFreshness(observation.GetKey(), observation.GetObservedAt(), observation.GetValidUntil(), now, true); err != nil {
		return false
	}
	return utf8.ValidString(observation.GetReason()) && len(observation.GetReason()) <= MaxReasonBytes
}

func ValidateSnapshot(snapshot *capabilityv1.CapabilitySnapshot, now time.Time) error {
	if snapshot == nil {
		return fmt.Errorf("capability snapshot is required")
	}
	if snapshot.GetNodeInstanceID() == "" || snapshot.GetSequence() <= 0 || snapshot.GetCollectedAt() == nil {
		return fmt.Errorf("capability snapshot node_instance_id, positive sequence, and collected_at are required")
	}
	if err := validateBoundedIdentity("node_instance_id", snapshot.GetNodeInstanceID()); err != nil {
		return err
	}
	if err := snapshot.GetCollectedAt().CheckValid(); err != nil {
		return fmt.Errorf("capability snapshot collected_at: %w", err)
	}
	if len(snapshot.GetObservations()) > MaxSnapshotObservations {
		return fmt.Errorf("capability snapshot exceeds %d observations", MaxSnapshotObservations)
	}
	if snapshot.GetCollectedAt().AsTime().After(now.Add(time.Minute)) {
		return fmt.Errorf("capability snapshot collected_at is in the future")
	}
	byKey, err := observationsByKey(snapshot.GetObservations())
	if err != nil {
		return err
	}
	for id, observation := range byKey {
		if err := validateObservation(id, observation, snapshot.GetCollectedAt().AsTime(), now); err != nil {
			return err
		}
	}
	for id, observation := range byKey {
		if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && !availableObservationSet(byKey, id, now, make(map[string]bool)) {
			return fmt.Errorf("available capability observation %q has an unavailable definition dependency", id)
		}
	}
	return nil
}

func validateObservation(id string, observation *capabilityv1.CapabilityObservation, collectedAt, now time.Time) error {
	if observation.GetProvider() == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_UNSPECIFIED || observation.GetObservedAt() == nil || !validCapabilityState(observation.GetState()) || !validCapabilityReasonCode(observation.GetReasonCode()) {
		return fmt.Errorf("capability observation %q has an invalid provider, state, reason code, or observed_at", id)
	}
	if err := observation.GetObservedAt().CheckValid(); err != nil {
		return fmt.Errorf("capability observation %q observed_at: %w", id, err)
	}
	if observation.GetValidUntil() != nil {
		if err := observation.GetValidUntil().CheckValid(); err != nil {
			return fmt.Errorf("capability observation %q valid_until: %w", id, err)
		}
	}
	provider, identity, err := ObservationOwner(observation.GetKey())
	if err != nil {
		return err
	}
	if observation.GetProvider() != provider {
		return fmt.Errorf("capability observation %q must be owned by %s", id, provider)
	}
	available := observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	if available != (observation.GetReasonCode() == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE) {
		return fmt.Errorf("capability observation %q has inconsistent state and reason code", id)
	}
	if !utf8.ValidString(observation.GetReason()) || len(observation.GetReason()) > MaxReasonBytes {
		return fmt.Errorf("capability observation %q reason exceeds %d bytes", id, MaxReasonBytes)
	}
	if observation.GetObservedAt().AsTime().After(collectedAt) || observation.GetObservedAt().AsTime().After(now.Add(time.Minute)) {
		return fmt.Errorf("capability observation %q was observed after publication", id)
	}
	if err := ValidateEvidence(observation.GetEvidence(), identity); err != nil {
		return fmt.Errorf("capability observation %q: %w", id, err)
	}
	if available {
		if err := validateFreshness(observation.GetKey(), observation.GetObservedAt(), observation.GetValidUntil(), now, false); err != nil {
			return fmt.Errorf("capability observation %q: %w", id, err)
		}
	} else if observation.GetValidUntil() != nil && observation.GetValidUntil().AsTime().Before(observation.GetObservedAt().AsTime()) {
		return fmt.Errorf("capability observation %q expires before it was observed", id)
	}
	return nil
}

// NormalizeObservation canonicalizes extension keys and bounds diagnostics. It
// intentionally does not mint IDs or hashes.
func NormalizeObservation(observation *capabilityv1.CapabilityObservation) {
	if observation == nil {
		return
	}
	if extension := observation.GetKey().GetExtension(); extension != nil {
		observation.Key = &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Extension{Extension: NormalizeExtension(extension)}}
	}
	observation.Reason = BoundedReason(observation.GetReason())
}

func CloneObservation(observation *capabilityv1.CapabilityObservation) *capabilityv1.CapabilityObservation {
	if observation == nil {
		return nil
	}
	return proto.Clone(observation).(*capabilityv1.CapabilityObservation)
}
