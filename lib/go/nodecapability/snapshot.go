package nodecapability

import (
	"fmt"
	"sort"
	"time"
	"unicode/utf8"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ResolveDependencies binds requirements to the exact proofs admitted from a
// single published snapshot. Duplicate and internal-fact requirements are
// rejected instead of being normalized away.
func ResolveDependencies(snapshot *capabilityv1.CapabilitySnapshot, keys []*capabilityv1.CapabilityKey, now time.Time) ([]*capabilityv1.CapabilityDependency, error) {
	if err := ValidateRequirementKeys(keys); err != nil {
		return nil, err
	}
	if err := ValidateSnapshot(snapshot, now); err != nil {
		return nil, fmt.Errorf("invalid capability snapshot: %w", err)
	}
	reference := &capabilityv1.CapabilitySnapshotReference{
		NodeInstanceID: snapshot.GetNodeInstanceID(),
		Sequence:       snapshot.GetSequence(),
		SnapshotID:     snapshot.GetSnapshotID(),
		CollectedAt:    cloneTimestamp(snapshot.GetCollectedAt()),
	}
	resolved := make([]*capabilityv1.CapabilityDependency, 0, len(keys))
	for _, key := range keys {
		id, _ := KeyID(key)
		observation, ok := AvailableObservation(snapshot, key, now)
		if !ok {
			return nil, fmt.Errorf("required capability %q is not available", id)
		}
		lossPolicy, err := LossPolicy(key)
		if err != nil {
			return nil, err
		}
		dependencyProofs := make([]*capabilityv1.CapabilityObservationProof, 0, len(observation.GetDependencies()))
		for _, proof := range observation.GetDependencies() {
			dependencyProofs = append(dependencyProofs, proto.Clone(proof).(*capabilityv1.CapabilityObservationProof))
		}
		resolved = append(resolved, &capabilityv1.CapabilityDependency{
			Key:                    CloneKey(key),
			LossPolicy:             lossPolicy,
			SelectedSnapshot:       proto.Clone(reference).(*capabilityv1.CapabilitySnapshotReference),
			SelectedObservation:    NewObservationProof(observation),
			DependencyObservations: dependencyProofs,
		})
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

func AvailableObservation(snapshot *capabilityv1.CapabilitySnapshot, key *capabilityv1.CapabilityKey, now time.Time) (*capabilityv1.CapabilityObservation, bool) {
	if snapshot == nil || key == nil || snapshot.GetCollectedAt() == nil || snapshot.GetCollectedAt().AsTime().After(now.Add(time.Minute)) {
		return nil, false
	}
	want, err := KeyID(key)
	if err != nil {
		return nil, false
	}
	byKey, err := observationsByKey(snapshot.GetObservations())
	if err != nil || !availableObservationSet(byKey, want, now) {
		return nil, false
	}
	return byKey[want], true
}

func observationsByKey(observations []*capabilityv1.CapabilityObservation) (map[string]*capabilityv1.CapabilityObservation, error) {
	byKey := make(map[string]*capabilityv1.CapabilityObservation, len(observations))
	extensionNames := make(map[string]struct{})
	for _, observation := range observations {
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

// availableObservationSet validates one bounded, flat proof set. The static
// catalog permits derived workload capabilities to depend only on base
// internal facts, so runtime evaluation never needs nested proof structures.
func availableObservationSet(byKey map[string]*capabilityv1.CapabilityObservation, id string, now time.Time) bool {
	observation := byKey[id]
	if !observationValid(observation, now) {
		return false
	}
	for _, proof := range observation.GetDependencies() {
		dependencyID, err := KeyID(proof.GetKey())
		if err != nil {
			return false
		}
		dependency := byKey[dependencyID]
		if !observationValid(dependency, now) || len(dependency.GetDependencies()) != 0 {
			return false
		}
		if !proto.Equal(NewObservationProof(dependency), proof) {
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
	if err != nil || observation.GetProvider() != provider || !dependenciesMatchCatalog(observation) {
		return false
	}
	if err := ValidateEvidence(observation.GetEvidence(), identity); err != nil {
		return false
	}
	if observation.GetObservationID() == "" || observation.GetObservationID() != ObservationID(observation) || observation.GetObservedAt().AsTime().After(now.Add(time.Minute)) {
		return false
	}
	if err := validateFreshness(observation.GetKey(), observation.GetObservedAt(), observation.GetValidUntil(), now, true); err != nil {
		return false
	}
	return len(observation.GetReason()) <= MaxReasonBytes
}

func ValidateSnapshot(snapshot *capabilityv1.CapabilitySnapshot, now time.Time) error {
	if snapshot == nil {
		return fmt.Errorf("capability snapshot is required")
	}
	if snapshot.GetNodeInstanceID() == "" || snapshot.GetSnapshotID() == "" || snapshot.GetSequence() <= 0 || snapshot.GetCollectedAt() == nil {
		return fmt.Errorf("capability snapshot identity, positive sequence, and collected_at are required")
	}
	if err := validateBoundedIdentity("node_instance_id", snapshot.GetNodeInstanceID()); err != nil {
		return err
	}
	if err := validateBoundedIdentity("snapshot_id", snapshot.GetSnapshotID()); err != nil {
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
		if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			continue
		}
		if !availableObservationSet(byKey, id, now) {
			return fmt.Errorf("available capability observation %q has invalid or unavailable dependency proof", id)
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
	if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && observation.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
		return fmt.Errorf("available capability observation %q must use the available reason code", id)
	}
	if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && observation.GetReasonCode() == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
		return fmt.Errorf("non-available capability observation %q cannot use the available reason code", id)
	}
	if !utf8.ValidString(observation.GetReason()) || len(observation.GetReason()) > MaxReasonBytes {
		return fmt.Errorf("capability observation %q reason exceeds %d bytes", id, MaxReasonBytes)
	}
	if observation.GetObservedAt().AsTime().After(collectedAt) {
		return fmt.Errorf("capability observation %q was observed after its snapshot was published", id)
	}
	if observation.GetObservedAt().AsTime().After(now.Add(time.Minute)) {
		return fmt.Errorf("capability observation %q has a future observed_at", id)
	}
	if observation.GetEvidence() != nil {
		if err := ValidateEvidence(observation.GetEvidence(), identity); err != nil {
			return fmt.Errorf("capability observation %q: %w", id, err)
		}
	} else if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		return fmt.Errorf("available capability observation %q is missing evidence", id)
	}
	if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		if err := validateFreshness(observation.GetKey(), observation.GetObservedAt(), observation.GetValidUntil(), now, false); err != nil {
			return fmt.Errorf("capability observation %q: %w", id, err)
		}
	} else if observation.GetValidUntil() != nil && observation.GetValidUntil().AsTime().Before(observation.GetObservedAt().AsTime()) {
		return fmt.Errorf("capability observation %q expires before it was observed", id)
	}
	if observation.GetObservationID() == "" || observation.GetObservationID() != ObservationID(observation) {
		return fmt.Errorf("capability observation %q has an invalid observation_id", id)
	}
	if len(observation.GetDependencies()) > MaxObservationProofs {
		return fmt.Errorf("capability observation %q has too many dependency proofs", id)
	}
	seen := make(map[string]struct{}, len(observation.GetDependencies()))
	for _, proof := range observation.GetDependencies() {
		proofID, err := KeyID(proof.GetKey())
		if err != nil {
			return fmt.Errorf("capability observation %q dependency: %w", id, err)
		}
		if _, duplicate := seen[proofID]; duplicate {
			return fmt.Errorf("capability observation %q has duplicate dependency %q", id, proofID)
		}
		seen[proofID] = struct{}{}
		if err := ValidateObservationProof(proof, now); err != nil {
			return fmt.Errorf("capability observation %q dependency %q: %w", id, proofID, err)
		}
	}
	if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && !dependenciesMatchCatalog(observation) {
		return fmt.Errorf("capability observation %q does not match catalog dependencies", id)
	}
	if identity == IdentityDerived && observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		if err := validateDerivedEvidenceDependencies(observation.GetEvidence(), observation.GetDependencies()); err != nil {
			return fmt.Errorf("derived capability observation %q: %w", id, err)
		}
		if expected := earliestExpiry(observation.GetDependencies()); !timestampsEqual(expected, observation.GetValidUntil()) {
			return fmt.Errorf("derived capability observation %q must inherit its earliest dependency expiry", id)
		}
	}
	return nil
}

func earliestExpiry(proofs []*capabilityv1.CapabilityObservationProof) *time.Time {
	var earliest *time.Time
	for _, proof := range proofs {
		if proof.GetValidUntil() == nil {
			continue
		}
		value := proof.GetValidUntil().AsTime()
		if earliest == nil || value.Before(*earliest) {
			copy := value
			earliest = &copy
		}
	}
	return earliest
}

func timestampsEqual(expected *time.Time, actual *timestamppb.Timestamp) bool {
	if expected == nil {
		return actual == nil
	}
	if actual == nil {
		return false
	}
	return expected.Equal(actual.AsTime())
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

func ValidateDependencySet(dependencies []*capabilityv1.CapabilityDependency, now time.Time) error {
	keys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	var selectedSnapshot *capabilityv1.CapabilitySnapshotReference
	for _, dependency := range dependencies {
		if dependency == nil {
			return fmt.Errorf("nil capability dependency")
		}
		keys = append(keys, dependency.GetKey())
	}
	if err := ValidateRequirementKeys(keys); err != nil {
		return err
	}
	for _, dependency := range dependencies {
		id, _ := KeyID(dependency.GetKey())
		policy, err := LossPolicy(dependency.GetKey())
		if err != nil || dependency.GetLossPolicy() != policy {
			return fmt.Errorf("capability dependency %q has invalid loss policy", id)
		}
		snapshot := dependency.GetSelectedSnapshot()
		if snapshot == nil || snapshot.GetNodeInstanceID() == "" || snapshot.GetSequence() <= 0 || snapshot.GetSnapshotID() == "" || snapshot.GetCollectedAt() == nil {
			return fmt.Errorf("capability dependency %q has invalid snapshot reference", id)
		}
		if err := validateBoundedIdentity("dependency node_instance_id", snapshot.GetNodeInstanceID()); err != nil {
			return fmt.Errorf("capability dependency %q: %w", id, err)
		}
		if err := validateBoundedIdentity("dependency snapshot_id", snapshot.GetSnapshotID()); err != nil {
			return fmt.Errorf("capability dependency %q: %w", id, err)
		}
		if err := snapshot.GetCollectedAt().CheckValid(); err != nil {
			return fmt.Errorf("capability dependency %q snapshot collected_at: %w", id, err)
		}
		if selectedSnapshot == nil {
			selectedSnapshot = proto.Clone(snapshot).(*capabilityv1.CapabilitySnapshotReference)
		} else if !proto.Equal(selectedSnapshot, snapshot) {
			return fmt.Errorf("capability dependency %q was selected from a different snapshot", id)
		}
		selectedAt := snapshot.GetCollectedAt().AsTime()
		if selectedAt.After(now.Add(time.Minute)) {
			return fmt.Errorf("capability dependency %q snapshot reference is from the future", id)
		}
		selectedProof := dependency.GetSelectedObservation()
		if err := ValidateObservationProof(selectedProof, selectedAt); err != nil {
			return fmt.Errorf("capability dependency %q selected proof: %w", id, err)
		}
		if selectedProof.GetObservedAt().AsTime().After(selectedAt) {
			return fmt.Errorf("capability dependency %q selected proof was observed after its snapshot", id)
		}
		if !proto.Equal(dependency.GetKey(), selectedProof.GetKey()) {
			return fmt.Errorf("capability dependency %q selected proof key mismatch", id)
		}
		if len(dependency.GetDependencyObservations()) > MaxObservationProofs {
			return fmt.Errorf("capability dependency %q has too many dependency proofs", id)
		}
		expectedKeys, _ := PlatformDependencyKeys(dependency.GetKey().GetPlatform())
		if dependency.GetKey().GetExtension() != nil {
			expectedKeys = nil
		}
		if !proofKeysMatch(expectedKeys, dependency.GetDependencyObservations()) {
			return fmt.Errorf("capability dependency %q proof set does not match catalog", id)
		}
		for _, proof := range dependency.GetDependencyObservations() {
			if err := ValidateObservationProof(proof, selectedAt); err != nil {
				return fmt.Errorf("capability dependency %q proof set: %w", id, err)
			}
			if proof.GetObservedAt().AsTime().After(selectedAt) {
				return fmt.Errorf("capability dependency %q proof set contains proof observed after its snapshot", id)
			}
		}
		if selectedProof.GetEvidence().GetDerived() != nil {
			if err := validateDerivedEvidenceDependencies(selectedProof.GetEvidence(), dependency.GetDependencyObservations()); err != nil {
				return fmt.Errorf("capability dependency %q: %w", id, err)
			}
			if expected := earliestExpiry(dependency.GetDependencyObservations()); !timestampsEqual(expected, selectedProof.GetValidUntil()) {
				return fmt.Errorf("capability dependency %q derived proof does not inherit dependency expiry", id)
			}
		}
	}
	return nil
}

func proofKeysMatch(expected []*capabilityv1.CapabilityKey, proofs []*capabilityv1.CapabilityObservationProof) bool {
	if len(expected) != len(proofs) {
		return false
	}
	seen := make(map[string]struct{}, len(proofs))
	for _, proof := range proofs {
		id, err := KeyID(proof.GetKey())
		if err != nil {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	for _, key := range expected {
		id, _ := KeyID(key)
		if _, ok := seen[id]; !ok {
			return false
		}
	}
	return true
}
