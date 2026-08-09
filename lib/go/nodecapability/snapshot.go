package nodecapability

import (
	"fmt"
	"sort"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

// ResolveDependencies binds requirements to the exact evidence admitted from
// one atomic snapshot. Callers persist the result with the allocation so later
// verification never has to reconstruct placement-time truth.
func ResolveDependencies(snapshot *capabilityv1.CapabilitySnapshot, keys []*capabilityv1.CapabilityKey, now time.Time) ([]*capabilityv1.CapabilityDependency, error) {
	resolved := make([]*capabilityv1.CapabilityDependency, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		id, err := KeyID(key)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		observation, ok := AvailableObservation(snapshot, key, now)
		if !ok {
			return nil, fmt.Errorf("required capability %q is not available", id)
		}
		lossPolicy, err := LossPolicy(key)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, &capabilityv1.CapabilityDependency{
			Key:                proto.Clone(key).(*capabilityv1.CapabilityKey),
			LossPolicy:         lossPolicy,
			SelectedEvidence:   proto.Clone(observation.GetEvidence()).(*capabilityv1.CapabilityEvidence),
			DependencyEvidence: cloneEvidenceReferences(observation.GetDependencies()),
		})
	}
	sort.Slice(resolved, func(i, j int) bool {
		left, _ := KeyID(resolved[i].GetKey())
		right, _ := KeyID(resolved[j].GetKey())
		return left < right
	})
	return resolved, nil
}

func cloneEvidenceReferences(in []*capabilityv1.CapabilityEvidenceReference) []*capabilityv1.CapabilityEvidenceReference {
	out := make([]*capabilityv1.CapabilityEvidenceReference, 0, len(in))
	for _, reference := range in {
		if reference != nil {
			out = append(out, proto.Clone(reference).(*capabilityv1.CapabilityEvidenceReference))
		}
	}
	return out
}

func AvailableObservation(snapshot *capabilityv1.CapabilitySnapshot, key *capabilityv1.CapabilityKey, now time.Time) (*capabilityv1.CapabilityObservation, bool) {
	if snapshot == nil || key == nil || snapshot.GetCollectedAt() == nil || snapshot.GetCollectedAt().AsTime().After(now.Add(time.Minute)) {
		return nil, false
	}
	want, err := KeyID(key)
	if err != nil {
		return nil, false
	}
	byKey := make(map[string]*capabilityv1.CapabilityObservation, len(snapshot.GetObservations()))
	for _, observation := range snapshot.GetObservations() {
		id, keyErr := KeyID(observation.GetKey())
		if keyErr != nil {
			continue
		}
		if _, duplicate := byKey[id]; duplicate {
			return nil, false
		}
		byKey[id] = observation
	}
	observation := byKey[want]
	if !availableObservationGraph(byKey, want, now, make(map[string]bool)) {
		return nil, false
	}
	return observation, true
}

func availableObservationGraph(byKey map[string]*capabilityv1.CapabilityObservation, id string, now time.Time, visiting map[string]bool) bool {
	if visiting[id] {
		return false
	}
	observation := byKey[id]
	if !observationValid(observation, now) {
		return false
	}
	visiting[id] = true
	defer delete(visiting, id)
	for _, dependency := range observation.GetDependencies() {
		dependencyID, err := KeyID(dependency.GetKey())
		if err != nil || !availableObservationGraph(byKey, dependencyID, now, visiting) {
			return false
		}
		resolved := byKey[dependencyID]
		if resolved.GetEvidence().GetEvidenceID() != dependency.GetEvidenceID() ||
			dependency.GetEvidenceID() != dependency.GetEvidence().GetEvidenceID() ||
			!proto.Equal(resolved.GetEvidence(), dependency.GetEvidence()) {
			return false
		}
	}
	return true
}

func observationValid(observation *capabilityv1.CapabilityObservation, now time.Time) bool {
	if observation == nil || observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || observation.GetObservedAt() == nil || observation.GetEvidence().GetEvidenceID() == "" {
		return false
	}
	expectedProvider, expectedScope, err := ObservationOwner(observation.GetKey())
	if err != nil || observation.GetProvider() != expectedProvider || observation.GetValidityScope() != expectedScope || !dependenciesMatchCatalog(observation) {
		return false
	}
	if observation.GetObservedAt().AsTime().After(now.Add(time.Minute)) {
		return false
	}
	evidence := observation.GetEvidence()
	switch observation.GetValidityScope() {
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC:
		return evidence.GetConfigDigest() != ""
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT:
		return evidence.GetBootID() != ""
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT:
		return evidence.GetBootID() != "" && evidence.GetMountIdentity() != ""
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME:
		return evidence.GetBootID() != "" && evidence.GetRuntimeName() != "" && evidence.GetRuntimeBinaryDigest() != "" && evidence.GetConfigDigest() != ""
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE:
		return observation.GetValidUntil() != nil && observation.GetValidUntil().AsTime().After(now)
	default:
		return false
	}
}

func ValidateSnapshot(snapshot *capabilityv1.CapabilitySnapshot, now time.Time) error {
	if snapshot == nil {
		return fmt.Errorf("capability snapshot is required")
	}
	if snapshot.GetNodeInstanceID() == "" || snapshot.GetSnapshotID() == "" || snapshot.GetSequence() <= 0 || snapshot.GetCollectedAt() == nil {
		return fmt.Errorf("capability snapshot identity, positive sequence, and collected_at are required")
	}
	if snapshot.GetCollectedAt().AsTime().After(now.Add(time.Minute)) {
		return fmt.Errorf("capability snapshot collected_at is in the future")
	}
	seen := make(map[string]struct{}, len(snapshot.GetObservations()))
	for _, observation := range snapshot.GetObservations() {
		id, err := KeyID(observation.GetKey())
		if err != nil {
			return err
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate capability observation %q", id)
		}
		seen[id] = struct{}{}
		if observation.GetProvider() == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_UNSPECIFIED || observation.GetValidityScope() == capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_UNSPECIFIED || observation.GetObservedAt() == nil {
			return fmt.Errorf("capability observation %q is missing provider, validity scope, or observed_at", id)
		}
		expectedProvider, expectedScope, err := ObservationOwner(observation.GetKey())
		if err != nil {
			return err
		}
		if observation.GetProvider() != expectedProvider || observation.GetValidityScope() != expectedScope {
			return fmt.Errorf("capability observation %q must be owned by %s with scope %s", id, expectedProvider, expectedScope)
		}
		if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED || observation.GetReasonCode() == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_UNSPECIFIED || observation.GetEvidence().GetEvidenceID() == "" {
			return fmt.Errorf("capability observation %q is missing state, reason code, or evidence identity", id)
		}
		if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && observation.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
			return fmt.Errorf("available capability observation %q must use the available reason code", id)
		}
		if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && observation.GetReasonCode() == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
			return fmt.Errorf("non-available capability observation %q cannot use the available reason code", id)
		}
		if observation.GetValidityScope() == capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE && observation.GetValidUntil() == nil {
			return fmt.Errorf("refreshable capability observation %q is missing valid_until", id)
		}
		if observation.GetValidUntil() != nil && observation.GetValidUntil().AsTime().Before(observation.GetObservedAt().AsTime()) {
			return fmt.Errorf("capability observation %q expires before it was observed", id)
		}
		if observation.GetObservedAt().AsTime().After(snapshot.GetCollectedAt().AsTime().Add(time.Minute)) {
			return fmt.Errorf("capability observation %q is newer than its snapshot", id)
		}
		dependencyKeys := make(map[string]struct{}, len(observation.GetDependencies()))
		for _, dependency := range observation.GetDependencies() {
			dependencyID, err := KeyID(dependency.GetKey())
			if err != nil {
				return fmt.Errorf("capability observation %q dependency: %w", id, err)
			}
			if _, duplicate := dependencyKeys[dependencyID]; duplicate {
				return fmt.Errorf("capability observation %q has duplicate dependency %q", id, dependencyID)
			}
			dependencyKeys[dependencyID] = struct{}{}
			if dependency.GetEvidenceID() == "" || dependency.GetEvidenceID() != dependency.GetEvidence().GetEvidenceID() {
				return fmt.Errorf("capability observation %q dependency %q has inconsistent evidence", id, dependencyID)
			}
		}
		if !dependenciesMatchCatalog(observation) {
			return fmt.Errorf("capability observation %q does not match catalog dependencies", id)
		}
	}
	for _, observation := range snapshot.GetObservations() {
		if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			continue
		}
		if _, ok := AvailableObservation(snapshot, observation.GetKey(), snapshot.GetCollectedAt().AsTime()); !ok {
			id, _ := KeyID(observation.GetKey())
			return fmt.Errorf("available capability observation %q has invalid or unavailable dependency evidence", id)
		}
	}
	return nil
}

func dependenciesMatchCatalog(observation *capabilityv1.CapabilityObservation) bool {
	if observation == nil {
		return false
	}
	expectedKeys := []*capabilityv1.CapabilityKey(nil)
	if observation.GetKey().GetExtension() == nil {
		var err error
		expectedKeys, err = PlatformDependencyKeys(observation.GetKey().GetPlatform())
		if err != nil {
			return false
		}
	}
	expected := make(map[string]struct{}, len(expectedKeys))
	for _, key := range expectedKeys {
		id, err := KeyID(key)
		if err != nil {
			return false
		}
		expected[id] = struct{}{}
	}
	if len(observation.GetDependencies()) != len(expected) {
		return false
	}
	for _, dependency := range observation.GetDependencies() {
		id, err := KeyID(dependency.GetKey())
		if err != nil {
			return false
		}
		if _, ok := expected[id]; !ok {
			return false
		}
		delete(expected, id)
	}
	return len(expected) == 0
}
