package allocation

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	runtimecontract "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type allocationState struct {
	record          *apipb.AllocationState
	runtime         *langruntime.LanguageRuntime
	imageMountRoots []*langruntime.RootFS
	workspace       workspaceImageRecord
}

type CapabilityConditionManifest struct {
	Attempt int64
	Set     *capabilityv1.CapabilityConditionSet
}

func newAllocationState(allocationID string) *allocationState {
	return &allocationState{record: &apipb.AllocationState{AllocationID: allocationID}}
}

func (h *Controller) stateLocked(allocationID string) *allocationState {
	state := h.allocationStates[allocationID]
	if state == nil {
		state = newAllocationState(allocationID)
		h.allocationStates[allocationID] = state
	}
	return state
}

func cloneAllocationRecord(record *apipb.AllocationState) *apipb.AllocationState {
	if record == nil {
		return nil
	}
	return proto.Clone(record).(*apipb.AllocationState)
}

func allocationRecordEmpty(record *apipb.AllocationState) bool {
	return record == nil || (record.GetRuntimeTemplate() == nil && len(record.GetImageMountUrls()) == 0 && record.GetWorkspaceImageUrl() == "" && len(record.GetCapabilityDependencies()) == 0 && record.GetCapabilityConditions() == nil && record.GetCapabilityAdmissionConditions() == nil && record.GetEnforcementManifest() == nil && record.GetCapabilityReconcile() == nil && record.GetLaunchVerification() == nil)
}

// ReplaceCapabilityAdmission atomically persists the admitted dependency
// proofs and their complete condition projection. The initial admission is the
// first durable side effect of create, before volumes, rootfs, mounts, cgroups,
// or runtime processes are touched. Post-create admission replaces both proof
// sets in the same write so recovery can never observe mismatched generations.
func (h *Controller) ReplaceCapabilityAdmission(allocationID string, attempt int64, requestDigest string, dependencies []*capabilityv1.CapabilityDependency, conditions []*capabilityv1.CapabilityCondition, observedAt time.Time) (*capabilityv1.CapabilityConditionSet, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || attempt <= 0 || !validStartRequestDigest(requestDigest) {
		return nil, errors.New("allocation id, positive attempt, and canonical request digest are required")
	}
	return h.replaceCapabilityAdmission(allocationID, attempt, requestDigest, dependencies, conditions, observedAt, true)
}

// ReplaceNodeLocalCapabilityAdmission persists the same proof and condition
// contract as a control-plane allocation without inventing a control-plane
// attempt. Node-local sandboxes are therefore covered by restart recovery,
// transition reconciliation, and periodic enforcement audits, while their
// conditions are never reported as an unknown controld allocation.
func (h *Controller) ReplaceNodeLocalCapabilityAdmission(allocationID, requestDigest string, dependencies []*capabilityv1.CapabilityDependency, conditions []*capabilityv1.CapabilityCondition, observedAt time.Time) (*capabilityv1.CapabilityConditionSet, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || !validStartRequestDigest(requestDigest) {
		return nil, errors.New("allocation id and canonical request digest are required")
	}
	return h.replaceCapabilityAdmission(allocationID, 0, requestDigest, dependencies, conditions, observedAt, false)
}

func (h *Controller) replaceCapabilityAdmission(allocationID string, attempt int64, requestDigest string, dependencies []*capabilityv1.CapabilityDependency, conditions []*capabilityv1.CapabilityCondition, observedAt time.Time, managed bool) (*capabilityv1.CapabilityConditionSet, error) {
	validationTime := time.Now().UTC()
	if err := capabilitycontract.ValidateDependencySet(dependencies, validationTime); err != nil {
		return nil, fmt.Errorf("validate allocation capability dependencies: %w", err)
	}
	canonical, err := canonicalCapabilityConditions(conditions)
	if err != nil {
		return nil, fmt.Errorf("canonicalize allocation capability conditions: %w", err)
	}
	if !capabilityConditionKeysEqualDependencies(dependencies, canonical) {
		return nil, fmt.Errorf("capability condition keys do not exactly match allocation dependencies")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	desired := &apipb.AllocationState{AllocationID: allocationID}
	h.stateMu.RLock()
	if current := h.allocationStates[allocationID]; current != nil {
		desired = cloneAllocationRecord(current.record)
	}
	h.stateMu.RUnlock()
	currentAttempt := desired.GetAllocationAttempt()
	if managed {
		if currentAttempt > 0 && currentAttempt != attempt {
			return nil, fmt.Errorf("allocation capability admission attempt %d conflicts with durable attempt %d", attempt, currentAttempt)
		}
		if currentAttempt == 0 && validStartRequestDigest(desired.GetAllocationRequestDigest()) {
			return nil, fmt.Errorf("managed capability admission conflicts with a durable node-local sandbox")
		}
	} else if currentAttempt > 0 {
		return nil, fmt.Errorf("node-local capability admission conflicts with durable managed attempt %d", currentAttempt)
	}
	if currentDigest := desired.GetAllocationRequestDigest(); currentDigest != "" && currentDigest != requestDigest {
		return nil, fmt.Errorf("allocation request digest conflicts with durable attempt contract")
	}
	if desired.GetLaunchVerification() != nil && desired.GetCapabilityAdmissionConditions() != nil {
		return nil, fmt.Errorf("allocation capability admission is already sealed")
	}
	revision := int64(1)
	if desired.GetCapabilityConditions() != nil {
		revision = desired.GetCapabilityConditions().GetRevision() + 1
	}
	set := &capabilityv1.CapabilityConditionSet{
		Revision:   revision,
		ObservedAt: timestamppb.New(observedAt.UTC()),
		Conditions: canonical,
	}
	if err := capabilitycontract.ValidateConditionSet(set, validationTime); err != nil {
		return nil, fmt.Errorf("validate allocation capability conditions: %w", err)
	}
	desired.CapabilityDependencies = cloneCapabilityDependencies(dependencies)
	desired.CapabilityConditions = set
	if desired.GetLaunchVerification() != nil {
		desired.CapabilityAdmissionConditions = proto.Clone(set).(*capabilityv1.CapabilityConditionSet)
	}
	if managed {
		desired.AllocationAttempt = attempt
	}
	desired.AllocationRequestDigest = requestDigest
	if err := h.persistAllocationRecord(desired); err != nil {
		return nil, fmt.Errorf("persist allocation capability admission: %w", err)
	}
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record = desired
	h.stateMu.Unlock()
	return proto.Clone(set).(*capabilityv1.CapabilityConditionSet), nil
}

// ManagedAllocationAttempt returns the durable control-plane generation for an
// allocation. Absence is distinct from attempt zero: managed attempts are
// always positive, while an absent record means the runtime has no generation
// to fence (for example, after an already-deleted sandbox is reconciled across
// a node restart).
func (h *Controller) ManagedAllocationAttempt(allocationID string) (int64, bool) {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	if state := h.allocationStates[strings.TrimSpace(allocationID)]; state != nil {
		attempt := state.record.GetAllocationAttempt()
		return attempt, attempt > 0
	}
	return 0, false
}

func (h *Controller) AllocationRequestDigest(allocationID string) string {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	if state := h.allocationStates[strings.TrimSpace(allocationID)]; state != nil {
		return state.record.GetAllocationRequestDigest()
	}
	return ""
}

func (h *Controller) CapabilityDependencyManifests() map[string][]*capabilityv1.CapabilityDependency {
	result := make(map[string][]*capabilityv1.CapabilityDependency)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	for allocationID, state := range h.allocationStates {
		if state != nil && len(state.record.GetCapabilityDependencies()) > 0 {
			result[allocationID] = cloneCapabilityDependencies(state.record.GetCapabilityDependencies())
		}
	}
	return result
}

func (h *Controller) CapabilityDependencies(allocationID string) []*capabilityv1.CapabilityDependency {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil {
		return nil
	}
	return cloneCapabilityDependencies(state.record.GetCapabilityDependencies())
}

// ReplaceCapabilityConditions persists and returns one complete, monotonically
// revised condition projection. Capability state never mutates allocation
// lifecycle state.
func (h *Controller) ReplaceCapabilityConditions(allocationID string, conditions []*capabilityv1.CapabilityCondition, observedAt time.Time) (*capabilityv1.CapabilityConditionSet, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil, errors.New("allocation id is required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	return h.replaceCapabilityConditionsLocked(allocationID, conditions, observedAt)
}

func (h *Controller) replaceCapabilityConditionsLocked(allocationID string, conditions []*capabilityv1.CapabilityCondition, observedAt time.Time) (*capabilityv1.CapabilityConditionSet, error) {
	canonical, err := canonicalCapabilityConditions(conditions)
	if err != nil {
		return nil, err
	}
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	desired := &apipb.AllocationState{AllocationID: allocationID}
	if current != nil {
		desired = cloneAllocationRecord(current.record)
	}
	h.stateMu.RUnlock()
	if !capabilityConditionKeysEqualDependencies(desired.GetCapabilityDependencies(), canonical) {
		return nil, fmt.Errorf("capability condition keys do not exactly match allocation dependencies")
	}
	revision := int64(1)
	if desired.GetCapabilityConditions() != nil {
		revision = desired.GetCapabilityConditions().GetRevision() + 1
	}
	set := &capabilityv1.CapabilityConditionSet{Revision: revision, ObservedAt: timestamppb.New(observedAt.UTC()), Conditions: canonical}
	if err := capabilitycontract.ValidateConditionSet(set, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("validate allocation capability conditions: %w", err)
	}
	desired.CapabilityConditions = set
	if err := h.persistAllocationRecord(desired); err != nil {
		return nil, fmt.Errorf("persist allocation capability conditions: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return proto.Clone(set).(*capabilityv1.CapabilityConditionSet), nil
}

func (h *Controller) MergeCapabilityReconcile(allocationID string, generation int64, keys []*capabilityv1.CapabilityKey) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || generation <= 0 {
		return errors.New("allocation id and positive reconcile generation are required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	if current == nil {
		h.stateMu.RUnlock()
		return fmt.Errorf("allocation %q has no durable state", allocationID)
	}
	desired := cloneAllocationRecord(current.record)
	h.stateMu.RUnlock()
	reconcile := desired.GetCapabilityReconcile()
	if reconcile == nil {
		reconcile = &apipb.AllocationCapabilityReconcileState{}
	} else {
		reconcile = proto.Clone(reconcile).(*apipb.AllocationCapabilityReconcileState)
	}
	byKey := make(map[string]*apipb.PendingCapabilityReconcile, len(reconcile.GetPending())+len(keys))
	for _, pending := range reconcile.GetPending() {
		id, err := capabilitycontract.KeyID(pending.GetKey())
		if err != nil {
			return fmt.Errorf("stored capability reconcile key: %w", err)
		}
		byKey[id] = proto.Clone(pending).(*apipb.PendingCapabilityReconcile)
	}
	for _, key := range keys {
		id, err := capabilitycontract.KeyID(key)
		if err != nil {
			return err
		}
		if pending := byKey[id]; pending == nil || pending.GetGeneration() < generation {
			byKey[id] = &apipb.PendingCapabilityReconcile{Key: capabilitycontract.CloneKey(key), Generation: generation}
		}
	}
	reconcile.Pending = reconcile.Pending[:0]
	for _, pending := range byKey {
		reconcile.Pending = append(reconcile.Pending, pending)
	}
	sort.Slice(reconcile.Pending, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(reconcile.Pending[i].GetKey())
		right, _ := capabilitycontract.KeyID(reconcile.Pending[j].GetKey())
		return left < right
	})
	reconcile.UpdatedAtUnixNano = time.Now().UTC().UnixNano()
	desired.CapabilityReconcile = reconcile
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist capability reconcile queue: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) CapabilityReconcileState(allocationID string) *apipb.AllocationCapabilityReconcileState {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record.GetCapabilityReconcile() == nil {
		return nil
	}
	return proto.Clone(state.record.GetCapabilityReconcile()).(*apipb.AllocationCapabilityReconcileState)
}

func (h *Controller) AckCapabilityReconcile(allocationID string, processed []*apipb.PendingCapabilityReconcile, terminating bool, lastErr error) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation id is required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	if current == nil {
		h.stateMu.RUnlock()
		return nil
	}
	desired := cloneAllocationRecord(current.record)
	h.stateMu.RUnlock()
	reconcile := desired.GetCapabilityReconcile()
	if reconcile == nil {
		return nil
	}
	reconcile = proto.Clone(reconcile).(*apipb.AllocationCapabilityReconcileState)
	acked := make(map[string]int64, len(processed))
	for _, pending := range processed {
		id, err := capabilitycontract.KeyID(pending.GetKey())
		if err != nil {
			return err
		}
		acked[id] = pending.GetGeneration()
	}
	remaining := reconcile.Pending[:0]
	for _, pending := range reconcile.GetPending() {
		id, _ := capabilitycontract.KeyID(pending.GetKey())
		if generation, ok := acked[id]; ok && pending.GetGeneration() <= generation {
			continue
		}
		remaining = append(remaining, pending)
	}
	reconcile.Pending = remaining
	reconcile.Terminating = terminating
	reconcile.LastError = ""
	if lastErr != nil {
		reconcile.LastError = capabilitycontract.BoundedReason(lastErr.Error())
	}
	reconcile.UpdatedAtUnixNano = time.Now().UTC().UnixNano()
	if len(reconcile.GetPending()) == 0 && !reconcile.GetTerminating() && reconcile.GetLastError() == "" {
		desired.CapabilityReconcile = nil
	} else {
		desired.CapabilityReconcile = reconcile
	}
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("ack capability reconcile queue: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return nil
}

// BeginCapabilityTermination durably transfers cleanup ownership to the
// allocation capability reconciler before the caller returns an enforcement
// failure. It is idempotent and aggregates repeated safety failures so
// concurrent capability loss never starts competing Delete workflows.
func (h *Controller) BeginCapabilityTermination(allocationID string, cause error) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || cause == nil {
		return errors.New("allocation id and capability termination cause are required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	if current == nil {
		h.stateMu.RUnlock()
		return fmt.Errorf("allocation %q has no durable state", allocationID)
	}
	desired := cloneAllocationRecord(current.record)
	h.stateMu.RUnlock()
	reconcile := desired.GetCapabilityReconcile()
	if reconcile == nil {
		reconcile = &apipb.AllocationCapabilityReconcileState{}
	} else {
		reconcile = proto.Clone(reconcile).(*apipb.AllocationCapabilityReconcileState)
	}
	reconcile.Terminating = true
	combined := cause
	if previous := strings.TrimSpace(reconcile.GetLastError()); previous != "" && !strings.Contains(previous, cause.Error()) {
		combined = errors.Join(errors.New(previous), cause)
	}
	reconcile.LastError = capabilitycontract.BoundedReason(combined.Error())
	reconcile.UpdatedAtUnixNano = time.Now().UTC().UnixNano()
	desired.CapabilityReconcile = reconcile
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist capability termination ownership: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) UpdateCapabilityCondition(allocationID string, condition *capabilityv1.CapabilityCondition, observedAt time.Time) (*capabilityv1.CapabilityConditionSet, error) {
	if condition == nil {
		return nil, errors.New("capability condition is required")
	}
	id, err := capabilitycontract.KeyID(condition.GetKey())
	if err != nil {
		return nil, err
	}
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil, errors.New("allocation id is required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	var conditions []*capabilityv1.CapabilityCondition
	if state := h.allocationStates[allocationID]; state != nil && state.record.GetCapabilityConditions() != nil {
		conditions = cloneCapabilityConditions(state.record.GetCapabilityConditions().GetConditions())
	}
	h.stateMu.RUnlock()
	replaced := false
	for index, existing := range conditions {
		existingID, keyErr := capabilitycontract.KeyID(existing.GetKey())
		if keyErr == nil && existingID == id {
			conditions[index] = proto.Clone(condition).(*capabilityv1.CapabilityCondition)
			replaced = true
			break
		}
	}
	if !replaced {
		conditions = append(conditions, proto.Clone(condition).(*capabilityv1.CapabilityCondition))
	}
	return h.replaceCapabilityConditionsLocked(allocationID, conditions, observedAt)
}

func (h *Controller) CapabilityConditions(allocationID string) *capabilityv1.CapabilityConditionSet {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record.GetCapabilityConditions() == nil {
		return nil
	}
	return proto.Clone(state.record.GetCapabilityConditions()).(*capabilityv1.CapabilityConditionSet)
}

func (h *Controller) CapabilityAdmissionConditions(allocationID string) *capabilityv1.CapabilityConditionSet {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record.GetCapabilityAdmissionConditions() == nil {
		return nil
	}
	return proto.Clone(state.record.GetCapabilityAdmissionConditions()).(*capabilityv1.CapabilityConditionSet)
}

func (h *Controller) CapabilityConditionManifests() map[string]CapabilityConditionManifest {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	result := make(map[string]CapabilityConditionManifest)
	for allocationID, state := range h.allocationStates {
		if state == nil || state.record.GetAllocationAttempt() <= 0 || state.record.GetCapabilityConditions() == nil {
			continue
		}
		result[allocationID] = CapabilityConditionManifest{
			Attempt: state.record.GetAllocationAttempt(),
			Set:     proto.Clone(state.record.GetCapabilityConditions()).(*capabilityv1.CapabilityConditionSet),
		}
	}
	return result
}

// StoreLaunchVerification atomically persists the immutable runtime launch
// manifest and the exact fail-stop requirements verified in the OCI
// create-before-start window. A manifest by itself is never treated as proof.
func (h *Controller) StoreLaunchVerification(allocationID string, manifest *apipb.AllocationEnforcementManifest, verified []*capabilityv1.CapabilityKey, observedAt time.Time) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation ID is required")
	}
	verification, err := newLaunchVerification(manifest, verified, observedAt, time.Now().UTC())
	if err != nil {
		return err
	}

	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	if current == nil {
		h.stateMu.RUnlock()
		return fmt.Errorf("allocation %q has no durable state", allocationID)
	}
	desired := cloneAllocationRecord(current.record)
	h.stateMu.RUnlock()
	if existing := desired.GetEnforcementManifest(); existing != nil && !proto.Equal(existing, manifest) {
		return fmt.Errorf("allocation %q enforcement manifest is immutable", allocationID)
	}
	if existing := desired.GetLaunchVerification(); existing != nil && !proto.Equal(existing, verification) {
		return fmt.Errorf("allocation %q launch verification is immutable", allocationID)
	}
	desired.EnforcementManifest = proto.Clone(manifest).(*apipb.AllocationEnforcementManifest)
	desired.LaunchVerification = verification
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist allocation launch verification: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return nil
}

func newLaunchVerification(manifest *apipb.AllocationEnforcementManifest, verified []*capabilityv1.CapabilityKey, observedAt, now time.Time) (*apipb.AllocationLaunchVerification, error) {
	if err := runtimecontract.ValidateEnforcementManifest(manifest, ""); err != nil {
		return nil, err
	}
	if observedAt.IsZero() || observedAt.After(now.Add(time.Second)) {
		return nil, errors.New("launch verification observed time is invalid")
	}
	canonical := make([]*capabilityv1.CapabilityKey, 0, len(verified))
	seen := make(map[string]struct{}, len(verified))
	for _, key := range verified {
		id, err := capabilitycontract.KeyID(key)
		if err != nil {
			return nil, fmt.Errorf("validate launch verification key: %w", err)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate launch verification key %q", id)
		}
		definition, ok := capabilitycontract.PlatformDefinition(key.GetPlatform())
		if key.GetExtension() != nil || !ok || definition.Audience != capabilitycontract.AudienceWorkloadRequirement || definition.LossPolicy != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP || definition.Verifier == capabilitycontract.VerifierNone {
			return nil, fmt.Errorf("capability %q is not a fail-stop allocation enforcement requirement", id)
		}
		seen[id] = struct{}{}
		canonical = append(canonical, capabilitycontract.CloneKey(key))
	}
	sort.Slice(canonical, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(canonical[i])
		right, _ := capabilitycontract.KeyID(canonical[j])
		return left < right
	})
	expected := launchVerificationRequirements(manifest)
	if !capabilitycontract.RequirementKeysEqual(canonical, expected) {
		return nil, fmt.Errorf("launch verification keys do not exactly match immutable enforcement manifest")
	}
	return &apipb.AllocationLaunchVerification{VerifiedCapabilities: canonical, VerifiedAtUnixNano: observedAt.UTC().UnixNano()}, nil
}

func launchVerificationRequirements(manifest *apipb.AllocationEnforcementManifest) []*capabilityv1.CapabilityKey {
	var required []*capabilityv1.CapabilityKey
	if manifest.GetMemoryLimitBytes() > 0 {
		platform := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT
		if manifest.GetRuntimeName() == config.RuntimeNameRunsc {
			platform = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT
		}
		required = append(required, capabilitycontract.PlatformKey(platform))
	}
	if manifest.GetEphemeralStorageLimitBytes() > 0 {
		platform := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT
		if manifest.GetRuntimeName() == config.RuntimeNameRunsc {
			platform = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT
		}
		required = append(required, capabilitycontract.PlatformKey(platform))
	}
	return required
}

func (h *Controller) LaunchVerification(allocationID string) *apipb.AllocationLaunchVerification {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record.GetLaunchVerification() == nil {
		return nil
	}
	return proto.Clone(state.record.GetLaunchVerification()).(*apipb.AllocationLaunchVerification)
}

func (h *Controller) EnforcementManifest(allocationID string) *apipb.AllocationEnforcementManifest {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record.GetEnforcementManifest() == nil {
		return nil
	}
	return proto.Clone(state.record.GetEnforcementManifest()).(*apipb.AllocationEnforcementManifest)
}

func canonicalCapabilityConditions(in []*capabilityv1.CapabilityCondition) ([]*capabilityv1.CapabilityCondition, error) {
	out := cloneCapabilityConditions(in)
	seen := make(map[string]struct{}, len(out))
	for _, condition := range out {
		id, err := capabilitycontract.KeyID(condition.GetKey())
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate capability condition %q", id)
		}
		seen[id] = struct{}{}
		condition.Message = capabilitycontract.BoundedReason(strings.TrimSpace(condition.GetMessage()))
		if condition.GetObservedAt() == nil {
			return nil, fmt.Errorf("capability condition %q is missing observed_at", id)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(out[i].GetKey())
		right, _ := capabilitycontract.KeyID(out[j].GetKey())
		return left < right
	})
	return out, nil
}

func capabilityConditionKeysEqualDependencies(dependencies []*capabilityv1.CapabilityDependency, conditions []*capabilityv1.CapabilityCondition) bool {
	if len(dependencies) != len(conditions) {
		return false
	}
	dependencyKeys := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		id, err := capabilitycontract.KeyID(dependency.GetKey())
		if err != nil {
			return false
		}
		if _, duplicate := dependencyKeys[id]; duplicate {
			return false
		}
		dependencyKeys[id] = struct{}{}
	}
	for _, condition := range conditions {
		id, err := capabilitycontract.KeyID(condition.GetKey())
		if err != nil {
			return false
		}
		if _, exists := dependencyKeys[id]; !exists {
			return false
		}
		delete(dependencyKeys, id)
	}
	return len(dependencyKeys) == 0
}

func cloneCapabilityConditions(in []*capabilityv1.CapabilityCondition) []*capabilityv1.CapabilityCondition {
	out := make([]*capabilityv1.CapabilityCondition, 0, len(in))
	for _, condition := range in {
		if condition != nil {
			out = append(out, proto.Clone(condition).(*capabilityv1.CapabilityCondition))
		}
	}
	return out
}

func cloneCapabilityDependencies(in []*capabilityv1.CapabilityDependency) []*capabilityv1.CapabilityDependency {
	out := make([]*capabilityv1.CapabilityDependency, 0, len(in))
	for _, dependency := range in {
		if dependency != nil {
			out = append(out, proto.Clone(dependency).(*capabilityv1.CapabilityDependency))
		}
	}
	return out
}

func (h *Controller) persistAllocationRecord(record *apipb.AllocationState) error {
	if record == nil || strings.TrimSpace(record.GetAllocationID()) == "" {
		return errors.New("allocation state requires an allocation id")
	}
	if allocationRecordEmpty(record) {
		return h.store.DeleteRecord(config.AllocationStateBucket, record.GetAllocationID())
	}
	return h.store.PutRecord(config.AllocationStateBucket, record.GetAllocationID(), record)
}

func (h *Controller) rememberContainerRuntime(allocationID string, runtime *langruntime.LanguageRuntime) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation id is required")
	}
	if runtime == nil || runtime.RuntimeTemplate() == nil {
		return errors.New("allocation runtime template is required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	if current != nil && current.runtime != nil && current.runtime != runtime {
		h.stateMu.RUnlock()
		return errors.New("allocation runtime is already registered")
	}
	var desired *apipb.AllocationState
	if current == nil {
		desired = &apipb.AllocationState{AllocationID: allocationID}
	} else {
		desired = cloneAllocationRecord(current.record)
	}
	h.stateMu.RUnlock()
	desired.RuntimeTemplate = proto.Clone(runtime.RuntimeTemplate()).(*apipb.RuntimeTemplate)
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist allocation runtime: %w", err)
	}
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record = desired
	state.runtime = runtime
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) rememberImageMountRoots(allocationID string, roots []*langruntime.RootFS, mounts []*apipb.ImageMount) error {
	allocationID = strings.TrimSpace(allocationID)
	if h == nil || allocationID == "" || len(roots) == 0 {
		return nil
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	if len(state.imageMountRoots) > 0 {
		h.stateMu.Unlock()
		return errors.New("allocation image mounts are already registered")
	}
	state.record.ImageMountUrls = state.record.ImageMountUrls[:0]
	for _, mount := range mounts {
		if mount != nil && strings.TrimSpace(mount.GetImage()) != "" {
			state.record.ImageMountUrls = append(state.record.ImageMountUrls, strings.TrimSpace(mount.GetImage()))
		}
	}
	state.imageMountRoots = append(state.imageMountRoots, roots...)
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) forgetImageMountRoots(allocationID string) {
	if h == nil || allocationID == "" {
		return
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	if state == nil {
		h.stateMu.RUnlock()
		return
	}
	desired := cloneAllocationRecord(state.record)
	roots := append([]*langruntime.RootFS(nil), state.imageMountRoots...)
	committed := state.runtime != nil
	h.stateMu.RUnlock()
	desired.ImageMountUrls = nil
	if committed {
		if err := h.persistAllocationRecord(desired); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Warn("persist released image mount ownership")
			return
		}
	}
	h.stateMu.Lock()
	state = h.allocationStates[allocationID]
	if state != nil {
		state.record = desired
		state.imageMountRoots = nil
		if allocationRecordEmpty(state.record) && state.workspace.cleanup == nil && state.runtime == nil {
			delete(h.allocationStates, allocationID)
		}
	}
	h.stateMu.Unlock()
	releaseImageMountRoots(roots)
}

func (h *Controller) rememberWorkspaceImage(allocationID string, workspace workspaceImageRecord) {
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	previous := state.workspace
	state.workspace = workspace
	h.stateMu.Unlock()
	if previous.cleanup != nil {
		previous.cleanup()
	}
}

func (h *Controller) rememberWorkspaceImageSpec(allocationID, imageURL, sourcePath, target string) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation id is required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record.WorkspaceImageUrl = strings.TrimSpace(imageURL)
	state.record.WorkspaceSourcePath = strings.TrimSpace(sourcePath)
	state.record.WorkspaceTarget = strings.TrimSpace(target)
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) forgetWorkspaceImage(allocationID string) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	if state == nil {
		h.stateMu.RUnlock()
		return
	}
	desired := cloneAllocationRecord(state.record)
	workspace := state.workspace
	committed := state.runtime != nil
	h.stateMu.RUnlock()
	desired.WorkspaceImageUrl = ""
	desired.WorkspaceSourcePath = ""
	desired.WorkspaceTarget = ""
	if committed {
		if err := h.persistAllocationRecord(desired); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Warn("persist released workspace image ownership")
			return
		}
	}
	h.stateMu.Lock()
	state = h.allocationStates[allocationID]
	if state != nil {
		state.record = desired
		state.workspace = workspaceImageRecord{}
		if allocationRecordEmpty(state.record) && len(state.imageMountRoots) == 0 && state.runtime == nil {
			delete(h.allocationStates, allocationID)
		}
	}
	h.stateMu.Unlock()
	if workspace.cleanup != nil {
		workspace.cleanup()
	}
}

func (h *Controller) releaseAllocationState(allocationID string) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	if err := h.store.DeleteRecord(config.AllocationStateBucket, allocationID); err != nil {
		return fmt.Errorf("delete allocation state: %w", err)
	}
	h.stateMu.Lock()
	state := h.allocationStates[allocationID]
	delete(h.allocationStates, allocationID)
	h.stateMu.Unlock()
	if state == nil {
		return nil
	}
	if state.runtime != nil {
		state.runtime.DecRef()
	}
	releaseImageMountRoots(state.imageMountRoots)
	if state.workspace.cleanup != nil {
		state.workspace.cleanup()
	}
	return nil
}

func (h *Controller) loadAllocationStates(runtimeInventory map[string]struct{}) error {
	records := make(map[string][]byte)
	if err := h.store.ForEachRecord(config.AllocationStateBucket, func(key string, value []byte) error {
		records[key] = append([]byte(nil), value...)
		return nil
	}); err != nil {
		return err
	}
	for id := range runtimeInventory {
		if _, ok := records[id]; !ok {
			return fmt.Errorf("live runtime container %s has no allocation recovery record", id)
		}
	}

	var recoveryErr error
	for key := range runtimeInventory {
		value := records[key]
		var record apipb.AllocationState
		if err := proto.Unmarshal(value, &record); err != nil {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("decode allocation state %s: %w", key, err))
			continue
		}
		if record.GetAllocationID() == "" || record.GetAllocationID() != key {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("allocation state key %s does not match record id %s", key, record.GetAllocationID()))
			continue
		}
		state, err := h.restoreAllocationState(&record)
		h.stateMu.Lock()
		h.allocationStates[key] = state
		h.stateMu.Unlock()
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("restore allocation %s state: %w", key, err))
		}
	}
	if recoveryErr != nil {
		return recoveryErr
	}

	var cleanupErr error
	for key := range records {
		if _, live := runtimeInventory[key]; live {
			continue
		}
		if err := h.store.DeleteRecord(config.AllocationStateBucket, key); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete orphan allocation state %s: %w", key, err))
		}
	}
	return cleanupErr
}

func (h *Controller) restoreAllocationState(record *apipb.AllocationState) (*allocationState, error) {
	state := &allocationState{record: cloneAllocationRecord(record)}
	var recoveryErr error
	if err := validateRecoveredCapabilityState(record, time.Now().UTC()); err != nil {
		recoveryErr = errors.Join(recoveryErr, err)
	}
	if record.GetRuntimeTemplate() == nil {
		recoveryErr = errors.Join(recoveryErr, errors.New("active allocation has no runtime template"))
	} else {
		rootfsConfig, err := langruntime.RootfsConfigFromRuntimeTemplate(record.GetRuntimeTemplate())
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
		} else {
			result, err := h.lrtManager.AddLangRuntime(context.Background(), record.GetRuntimeTemplate(), rootfsConfig, true)
			if err != nil {
				recoveryErr = errors.Join(recoveryErr, err)
			} else {
				state.runtime = result.Runtime
				state.runtime.IncRef()
			}
		}
	}
	if err := h.restoreAllocationImages(record, state); err != nil {
		recoveryErr = errors.Join(recoveryErr, err)
	}
	return state, recoveryErr
}

func validateRecoveredCapabilityState(record *apipb.AllocationState, now time.Time) error {
	if record == nil {
		return errors.New("allocation recovery record is required")
	}
	dependencies := record.GetCapabilityDependencies()
	if err := capabilitycontract.ValidateDependencySet(dependencies, now); err != nil {
		return fmt.Errorf("validate recovered capability dependencies: %w", err)
	}
	conditions := record.GetCapabilityConditions()
	governed := record.GetAllocationAttempt() > 0 || len(dependencies) > 0 || record.GetAllocationRequestDigest() != ""
	if record.GetAllocationAttempt() > 0 && conditions == nil {
		return errors.New("active managed allocation is missing its durable capability condition set")
	}
	if (record.GetAllocationAttempt() > 0 || len(dependencies) > 0) && !validStartRequestDigest(record.GetAllocationRequestDigest()) {
		return errors.New("active capability-governed allocation is missing its canonical request digest")
	}
	if conditions != nil {
		if err := capabilitycontract.ValidateConditionSet(conditions, now); err != nil {
			return fmt.Errorf("validate recovered capability conditions: %w", err)
		}
		if !capabilityConditionKeysEqualDependencies(dependencies, conditions.GetConditions()) {
			return errors.New("recovered capability conditions do not exactly match dependencies")
		}
	}
	admissionConditions := record.GetCapabilityAdmissionConditions()
	if governed && admissionConditions == nil {
		return errors.New("active capability-governed allocation is missing its sealed create condition proof")
	}
	if admissionConditions != nil {
		if err := validateSealedCapabilityAdmission(dependencies, admissionConditions, now); err != nil {
			return fmt.Errorf("validate sealed create capability admission: %w", err)
		}
	}
	manifest := record.GetEnforcementManifest()
	verification := record.GetLaunchVerification()
	if manifest == nil || verification == nil {
		return errors.New("active allocation is missing its atomic launch enforcement proof")
	}
	if verification.GetVerifiedAtUnixNano() <= 0 {
		return errors.New("recovered launch enforcement proof has no verified time")
	}
	verifiedAt := time.Unix(0, verification.GetVerifiedAtUnixNano()).UTC()
	expected, err := newLaunchVerification(manifest, verification.GetVerifiedCapabilities(), verifiedAt, now)
	if err != nil {
		return fmt.Errorf("validate recovered launch enforcement proof: %w", err)
	}
	if !proto.Equal(expected, verification) {
		return errors.New("recovered launch enforcement proof is not canonical")
	}
	if err := validateCapabilityReconcileState(record.GetCapabilityReconcile(), dependencies, now); err != nil {
		return fmt.Errorf("validate recovered capability reconcile state: %w", err)
	}
	return nil
}

func validateSealedCapabilityAdmission(dependencies []*capabilityv1.CapabilityDependency, set *capabilityv1.CapabilityConditionSet, now time.Time) error {
	if set == nil || set.GetRevision() != 2 {
		return errors.New("sealed create capability condition proof must be revision 2")
	}
	if err := capabilitycontract.ValidateConditionSet(set, now); err != nil {
		return err
	}
	if !capabilityConditionKeysEqualDependencies(dependencies, set.GetConditions()) {
		return errors.New("sealed create capability conditions do not exactly match dependencies")
	}
	byKey := make(map[string]*capabilityv1.CapabilityDependency, len(dependencies))
	for _, dependency := range dependencies {
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		byKey[id] = dependency
	}
	for _, condition := range set.GetConditions() {
		id, _ := capabilitycontract.KeyID(condition.GetKey())
		dependency := byKey[id]
		if condition.GetState() != capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY || condition.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
			return fmt.Errorf("sealed create capability condition %q is not healthy", id)
		}
		if dependency == nil || !proto.Equal(condition.GetProof(), dependency.GetSelectedObservation()) {
			return fmt.Errorf("sealed create capability condition %q does not bind admitted proof", id)
		}
	}
	return nil
}

func validateCapabilityReconcileState(state *apipb.AllocationCapabilityReconcileState, dependencies []*capabilityv1.CapabilityDependency, now time.Time) error {
	if state == nil {
		return nil
	}
	if state.GetUpdatedAtUnixNano() <= 0 || time.Unix(0, state.GetUpdatedAtUnixNano()).After(now.Add(time.Minute)) {
		return errors.New("capability reconcile updated time is invalid")
	}
	if len(state.GetLastError()) > capabilitycontract.MaxReasonBytes {
		return errors.New("capability reconcile error exceeds its bounded payload")
	}
	allowed := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		allowed[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(state.GetPending()))
	for _, pending := range state.GetPending() {
		if pending == nil || pending.GetGeneration() <= 0 {
			return errors.New("pending capability reconcile requires a positive generation")
		}
		id, err := capabilitycontract.KeyID(pending.GetKey())
		if err != nil {
			return err
		}
		if _, ok := allowed[id]; !ok {
			return fmt.Errorf("pending capability %q is not an allocation dependency", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate pending capability %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (h *Controller) restoreAllocationImages(record *apipb.AllocationState, state *allocationState) error {
	for _, imageURL := range record.GetImageMountUrls() {
		rootfs, err := h.acquireRecoveredImageRoot(imageURL)
		if err != nil {
			return err
		}
		state.imageMountRoots = append(state.imageMountRoots, rootfs)
	}
	if record.GetWorkspaceImageUrl() == "" {
		return nil
	}
	if err := validateWorkspaceImage(&apipb.WorkspaceImageSource{
		Variants:   []*apipb.WorkspaceImageVariant{{Format: "oci", Image: record.GetWorkspaceImageUrl()}},
		SourcePath: record.GetWorkspaceSourcePath(),
		Target:     record.GetWorkspaceTarget(),
	}); err != nil {
		return err
	}
	rootfs, err := h.acquireRecoveredImageRoot(record.GetWorkspaceImageUrl())
	if err != nil {
		return err
	}
	workspaceRoot := filepath.Join(h.config.RuntimeConfig.FilestoreDir, workspaceViewsDir, record.GetAllocationID())
	lower, err := workspaceLowerPath(rootfs.Path(), record.GetWorkspaceSourcePath())
	if err != nil {
		state.imageMountRoots = append(state.imageMountRoots, rootfs)
		return err
	}
	merged, err := restoreWorkspaceCOW(workspaceRoot, lower)
	if err != nil {
		state.imageMountRoots = append(state.imageMountRoots, rootfs)
		return err
	}
	state.workspace = workspaceImageRecord{
		payloadRoot: rootfs.Path(),
		taskRoot:    strings.TrimSuffix(path.Clean(record.GetWorkspaceSourcePath()), "/workspace"),
		merged:      merged,
		target:      record.GetWorkspaceTarget(),
		cleanup: func() {
			if err := cleanupWorkspaceCOW(workspaceRoot); err != nil {
				logrus.WithError(err).Warn("cleanup recovered workspace view")
			}
			rootfs.ReleaseActiveRef()
		},
	}
	return nil
}

func (h *Controller) acquireRecoveredImageRoot(imageURL string) (*langruntime.RootFS, error) {
	config, err := h.lrtManager.ResolveRootfsConfig(langruntime.RootfsConfig{SrcType: apipb.RootfsSrcType_IMAGE, ImageUrl: imageURL})
	if err != nil {
		return nil, err
	}
	rootfs, err := h.lrtManager.GetRootfs(config)
	if err != nil {
		return nil, err
	}
	if err := rootfs.IncActiveRef(); err != nil {
		return nil, err
	}
	return rootfs, nil
}
