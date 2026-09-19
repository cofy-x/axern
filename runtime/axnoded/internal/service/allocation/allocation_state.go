package allocation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	runtimecontract "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type allocationState struct {
	record          *apipb.AllocationState
	runtime         *environmentcache.PreparedEnvironment
	imageMountRoots []*environmentcache.RootFS
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
	return record == nil || (record.GetNodeID() == "" && record.GetAllocationRequestDigest() == "" && record.GetExecutionLeaseExpiresAtUnixNano() == 0 && record.GetTerminationDiagnosticCode() == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED && record.GetTerminationMessage() == "" && record.GetEnvironment() == nil && record.GetResources() == nil && len(record.GetImageMountUrls()) == 0 && len(record.GetCapabilityRequirements()) == 0 && len(record.GetDeclaredOutputs()) == 0 && record.GetRootfsSnapshot() == nil && record.GetEnforcementManifest() == nil && record.GetCapabilityReconcile() == nil)
}

func (h *Controller) HasAllocation(allocationID string) bool {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	return state != nil && state.record != nil && state.record.GetAllocationID() == strings.TrimSpace(allocationID)
}

func (h *Controller) AllocationIDs() []string {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	ids := make([]string, 0, len(h.allocationStates))
	for allocationID, state := range h.allocationStates {
		if state != nil && state.record != nil && state.record.GetAllocationID() == allocationID {
			ids = append(ids, allocationID)
		}
	}
	sort.Strings(ids)
	return ids
}

// ResolvedEnvironmentID returns the template referenced by the admitted Allocation
// record. It feeds rebuildable locality observations without consulting OCI
// metadata or labels.
func (h *Controller) ResolvedEnvironmentID(allocationID string) string {
	if h == nil {
		return ""
	}
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record == nil || state.record.GetEnvironment() == nil {
		return ""
	}
	return strings.TrimSpace(state.record.GetEnvironment().GetID())
}

// RecoveryRecords is the validated node-local recovery view. Intents contains
// every durable create intent. EnforcementVerified is the subset whose
// runtime enforcement manifest crossed the create-before-start barrier and may
// therefore recover a running or terminal runtime container.
type RecoveryRecords struct {
	Intents             map[string]struct{}
	EnforcementVerified map[string]struct{}
}

// InspectRecoveryRecords validates durable create intents without acquiring
// runtime or image ownership. An intent without verified enforcement is not
// corrupt: axnoded may have stopped after admission but before OCI activation.
// Startup reconciles those records against the authoritative runsc inventory.
func (h *Controller) InspectRecoveryRecords() (RecoveryRecords, error) {
	result := RecoveryRecords{
		Intents:             make(map[string]struct{}),
		EnforcementVerified: make(map[string]struct{}),
	}
	if h == nil || h.store == nil {
		return result, nil
	}
	now := time.Now().UTC()
	err := h.store.ForEachRecord(config.AllocationStateBucket, func(key string, value []byte) error {
		var record apipb.AllocationState
		if err := proto.Unmarshal(value, &record); err != nil {
			return fmt.Errorf("decode allocation state %s: %w", key, err)
		}
		if record.GetAllocationID() == "" || record.GetAllocationID() != key {
			return fmt.Errorf("allocation state key %s does not match record id %s", key, record.GetAllocationID())
		}
		enforcementVerified, err := classifyRecoveryRecord(&record, now)
		if err != nil {
			return fmt.Errorf("validate allocation state %s: %w", key, err)
		}
		if strings.TrimSpace(record.GetNodeID()) == "" {
			return fmt.Errorf("allocation state %s has no admitted node", key)
		}
		result.Intents[key] = struct{}{}
		if enforcementVerified {
			result.EnforcementVerified[key] = struct{}{}
		}
		return nil
	})
	return result, err
}

// StoreAllocationIntent persists the immutable node execution contract as the
// first create side effect. Node observations, effective runtime projection,
// and conditions are rebuildable and are never copied into this record.
func (h *Controller) StoreAllocationIntent(allocationID, nodeID, requestDigest string, executionLeaseExpiresAt time.Time, resourceSpec *commonv1.ResourceSpec, requirements []*capabilityv1.CapabilityRequirement, declaredOutputs []*commonv1.DeclaredOutput, rootfsSnapshot *commonv1.RootfsSnapshot) error {
	allocationID = strings.TrimSpace(allocationID)
	nodeID = strings.TrimSpace(nodeID)
	if allocationID == "" || !validStartRequestDigest(requestDigest) {
		return errors.New("allocation id and canonical request digest are required")
	}
	if nodeID != "" && !executionLeaseExpiresAt.After(time.Now()) {
		return errors.New("control-plane allocation requires a future execution lease deadline")
	}
	if err := capabilitycontract.ValidateRequirements(requirements); err != nil {
		return fmt.Errorf("validate allocation capability requirements: %w", err)
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	desired := &apipb.AllocationState{AllocationID: allocationID}
	h.stateMu.RLock()
	if current := h.allocationStates[allocationID]; current != nil {
		desired = cloneAllocationRecord(current.record)
	}
	h.stateMu.RUnlock()
	if currentDigest := desired.GetAllocationRequestDigest(); currentDigest != "" && currentDigest != requestDigest {
		return fmt.Errorf("allocation request digest conflicts with durable contract")
	}
	if currentNodeID := desired.GetNodeID(); currentNodeID != "" && currentNodeID != nodeID {
		return fmt.Errorf("allocation node binding conflicts with durable contract")
	}
	desired.NodeID = nodeID
	desired.CapabilityRequirements = cloneCapabilityRequirements(requirements)
	desired.DeclaredOutputs = cloneDeclaredOutputs(declaredOutputs)
	if rootfsSnapshot != nil {
		desired.RootfsSnapshot = proto.Clone(rootfsSnapshot).(*commonv1.RootfsSnapshot)
	} else {
		desired.RootfsSnapshot = nil
	}
	if resourceSpec != nil {
		desired.Resources = proto.Clone(resourceSpec).(*commonv1.ResourceSpec)
	} else {
		desired.Resources = nil
	}
	desired.AllocationRequestDigest = requestDigest
	if nodeID != "" {
		desired.ExecutionLeaseExpiresAtUnixNano = executionLeaseExpiresAt.UTC().UnixNano()
	}
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist allocation intent: %w", err)
	}
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record = desired
	h.stateMu.Unlock()
	return nil
}

func cloneDeclaredOutputs(in []*commonv1.DeclaredOutput) []*commonv1.DeclaredOutput {
	out := make([]*commonv1.DeclaredOutput, 0, len(in))
	for _, declared := range in {
		if declared != nil {
			out = append(out, proto.Clone(declared).(*commonv1.DeclaredOutput))
		}
	}
	return out
}

// RenewExecutionLeases applies only explicitly granted authority. Absence is
// not revocation: an in-flight response may precede another Allocation create.
// Missing grants expire at their existing finite deadline; cancellation owns
// explicit cleanup. Expired authority cannot be revived by a late response.
func (h *Controller) RenewExecutionLeases(ttls map[string]time.Duration, receivedAt time.Time) error {
	if h == nil {
		return nil
	}
	h.stateMu.RLock()
	ids := make([]string, 0, len(h.allocationStates))
	for allocationID, state := range h.allocationStates {
		if state != nil && strings.TrimSpace(state.record.GetNodeID()) != "" && ttls[allocationID] > 0 {
			ids = append(ids, allocationID)
		}
	}
	h.stateMu.RUnlock()
	for _, allocationID := range ids {
		expiresAt := receivedAt.Add(ttls[allocationID]).UTC().UnixNano()
		unlock := h.recordMutationLocks.Lock(allocationID)
		h.stateMu.RLock()
		state := h.allocationStates[allocationID]
		if state == nil {
			h.stateMu.RUnlock()
			unlock()
			continue
		}
		desired := cloneAllocationRecord(state.record)
		h.stateMu.RUnlock()
		currentDeadline := desired.GetExecutionLeaseExpiresAtUnixNano()
		if currentDeadline <= receivedAt.UnixNano() || expiresAt <= currentDeadline {
			unlock()
			continue
		}
		desired.ExecutionLeaseExpiresAtUnixNano = expiresAt
		if err := h.persistAllocationRecord(desired); err != nil {
			unlock()
			return fmt.Errorf("persist allocation %s execution lease: %w", allocationID, err)
		}
		h.stateMu.Lock()
		if current := h.allocationStates[allocationID]; current != nil {
			current.record = desired
		}
		h.stateMu.Unlock()
		unlock()
	}
	return nil
}

func (h *Controller) ExpiredExecutionLeaseAllocationIDs(now time.Time) []string {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	var ids []string
	for allocationID, state := range h.allocationStates {
		if state == nil || strings.TrimSpace(state.record.GetNodeID()) == "" {
			continue
		}
		expires := state.record.GetExecutionLeaseExpiresAtUnixNano()
		if expires <= 0 || !time.Unix(0, expires).After(now) {
			ids = append(ids, allocationID)
		}
	}
	sort.Strings(ids)
	return ids
}

// MarkTerminationIntent durably records why node-owned cleanup must stop an
// Allocation. It is intentionally narrow: the Allocation state machine remains
// control-plane owned, while this record survives a node crash between deciding
// to fail closed and observing the runtime exit.
func (h *Controller) MarkTerminationIntent(allocationID string, diagnosticCode commonv1.WorkloadDiagnosticCode, message string) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || diagnosticCode == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return errors.New("allocation id and termination diagnostic code are required")
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	if state == nil || state.record == nil {
		h.stateMu.RUnlock()
		return fmt.Errorf("allocation %q has no durable recovery record", allocationID)
	}
	desired := cloneAllocationRecord(state.record)
	h.stateMu.RUnlock()
	if desired.GetTerminationDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		return nil
	}
	desired.TerminationDiagnosticCode = diagnosticCode
	desired.TerminationMessage = strings.TrimSpace(message)
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist allocation termination intent: %w", err)
	}
	h.stateMu.Lock()
	if current := h.allocationStates[allocationID]; current != nil {
		current.record = desired
	}
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) TerminationIntent(allocationID string) (commonv1.WorkloadDiagnosticCode, string) {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record == nil {
		return commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED, ""
	}
	return state.record.GetTerminationDiagnosticCode(), state.record.GetTerminationMessage()
}

// ResourceSpec returns the immutable scheduler and enforcement inputs for an
// admitted Allocation. Runtime status and OCI metadata are not specification
// authorities.
func (h *Controller) ResourceSpec(allocationID string) *commonv1.ResourceSpec {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record == nil || state.record.GetResources() == nil {
		return nil
	}
	return proto.Clone(state.record.GetResources()).(*commonv1.ResourceSpec)
}

func (h *Controller) AllocationRequestDigest(allocationID string) string {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	if state := h.allocationStates[strings.TrimSpace(allocationID)]; state != nil {
		return state.record.GetAllocationRequestDigest()
	}
	return ""
}

func (h *Controller) CapabilityRequirementManifests() map[string][]*capabilityv1.CapabilityRequirement {
	result := make(map[string][]*capabilityv1.CapabilityRequirement)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	for allocationID, state := range h.allocationStates {
		if state != nil && len(state.record.GetCapabilityRequirements()) > 0 {
			result[allocationID] = cloneCapabilityRequirements(state.record.GetCapabilityRequirements())
		}
	}
	return result
}

func (h *Controller) CapabilityRequirements(allocationID string) []*capabilityv1.CapabilityRequirement {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil {
		return nil
	}
	return cloneCapabilityRequirements(state.record.GetCapabilityRequirements())
}

// ReplaceCapabilityConditions validates and returns a rebuildable diagnostic
// projection. Conditions are not node-local durable facts.
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
	state := h.allocationStates[allocationID]
	var requirements []*capabilityv1.CapabilityRequirement
	if state != nil {
		requirements = cloneCapabilityRequirements(state.record.GetCapabilityRequirements())
	}
	h.stateMu.RUnlock()
	if !capabilityConditionKeysEqualDependencies(requirements, canonical) {
		return nil, fmt.Errorf("capability condition keys do not exactly match allocation dependencies")
	}
	set := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(observedAt.UTC()), Conditions: canonical}
	if err := capabilitycontract.ValidateConditionSet(set, time.Now().UTC()); err != nil {
		return nil, fmt.Errorf("validate allocation capability conditions: %w", err)
	}
	return proto.Clone(set).(*capabilityv1.CapabilityConditionSet), nil
}

func (h *Controller) MergeCapabilityReconcile(allocationID string) error {
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
	if reconcile.GetPendingIntentSequence() == math.MaxInt64 {
		return errors.New("capability reconcile intent sequence exhausted")
	}
	reconcile.PendingIntentSequence++
	desired.CapabilityReconcile = reconcile
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist capability reconcile intent: %w", err)
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

func (h *Controller) AckCapabilityReconcile(allocationID string, processedSequence int64, terminating bool, lastErr error) error {
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
	if processedSequence == reconcile.GetPendingIntentSequence() {
		reconcile.PendingIntentSequence = 0
	}
	reconcile.Terminating = terminating
	reconcile.LastError = ""
	if lastErr != nil {
		reconcile.LastError = capabilitycontract.BoundedReason(lastErr.Error())
	}
	if reconcile.GetPendingIntentSequence() == 0 && !reconcile.GetTerminating() && reconcile.GetLastError() == "" {
		desired.CapabilityReconcile = nil
	} else {
		desired.CapabilityReconcile = reconcile
	}
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("ack capability reconcile intent: %w", err)
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
	desired.CapabilityReconcile = reconcile
	if desired.GetTerminationDiagnosticCode() == commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED {
		desired.TerminationDiagnosticCode = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_CAPABILITY_ENFORCEMENT_LOST
		desired.TerminationMessage = "allocation capability enforcement was lost"
	}
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist capability termination ownership: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return nil
}

// StoreVerifiedEnforcementManifest persists the immutable runtime contract
// only after every fail-stop requirement has passed the create-before-start gate.
func (h *Controller) StoreVerifiedEnforcementManifest(allocationID string, manifest *apipb.AllocationEnforcementManifest, verified []*capabilityv1.CapabilityKey, observedAt time.Time) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation ID is required")
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
	verifiedManifest, err := verifiedEnforcementManifest(
		manifest,
		verified,
		desired.GetCapabilityRequirements(),
		observedAt,
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	if existing := desired.GetEnforcementManifest(); existing != nil && !proto.Equal(existing, verifiedManifest) {
		return fmt.Errorf("allocation %q enforcement manifest is immutable", allocationID)
	}
	desired.EnforcementManifest = verifiedManifest
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist verified allocation enforcement manifest: %w", err)
	}
	h.stateMu.Lock()
	h.stateLocked(allocationID).record = desired
	h.stateMu.Unlock()
	return nil
}

func verifiedEnforcementManifest(manifest *apipb.AllocationEnforcementManifest, verified []*capabilityv1.CapabilityKey, dependencies []*capabilityv1.CapabilityRequirement, observedAt, now time.Time) (*apipb.AllocationEnforcementManifest, error) {
	if err := runtimecontract.ValidateEnforcementManifest(manifest, ""); err != nil {
		return nil, err
	}
	if observedAt.IsZero() || observedAt.After(now.Add(time.Second)) {
		return nil, errors.New("enforcement verification observed time is invalid")
	}
	canonical := make([]*capabilityv1.CapabilityKey, 0, len(verified))
	seen := make(map[string]struct{}, len(verified))
	for _, key := range verified {
		id, err := capabilitycontract.KeyID(key)
		if err != nil {
			return nil, fmt.Errorf("validate enforcement verification key: %w", err)
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("duplicate enforcement verification key %q", id)
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
	expected, err := RequiredEnforcementKeys(manifest, dependencies)
	if err != nil {
		return nil, err
	}
	if !capabilitycontract.RequirementKeysEqual(canonical, expected) {
		return nil, fmt.Errorf("verified capabilities do not exactly match immutable enforcement contract")
	}
	return proto.Clone(manifest).(*apipb.AllocationEnforcementManifest), nil
}

func RequiredEnforcementKeys(manifest *apipb.AllocationEnforcementManifest, dependencies []*capabilityv1.CapabilityRequirement) ([]*capabilityv1.CapabilityKey, error) {
	required := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency == nil || dependency.GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
			continue
		}
		key := dependency.GetKey()
		if _, err := capabilitycontract.KeyID(key); err != nil {
			return nil, fmt.Errorf("validate immutable fail-stop dependency: %w", err)
		}
		required = append(required, capabilitycontract.CloneKey(key))
	}

	manifestRequired := make([]*capabilityv1.CapabilityKey, 0, 2)
	if manifest.GetMemoryLimitBytes() > 0 {
		platform := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT
		manifestRequired = append(manifestRequired, capabilitycontract.PlatformKey(platform))
	}
	if manifest.GetEphemeralStorageLimitBytes() > 0 {
		platform := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT
		manifestRequired = append(manifestRequired, capabilitycontract.PlatformKey(platform))
	}
	for _, key := range manifestRequired {
		found := false
		for _, candidate := range required {
			if capabilitycontract.RequirementKeysEqual([]*capabilityv1.CapabilityKey{key}, []*capabilityv1.CapabilityKey{candidate}) {
				found = true
				break
			}
		}
		if !found {
			required = append(required, key)
		}
	}
	return required, nil
}

func (h *Controller) VerifiedEnforcementManifest(allocationID string) *apipb.AllocationEnforcementManifest {
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	state := h.allocationStates[strings.TrimSpace(allocationID)]
	if state == nil || state.record.GetEnforcementManifest() == nil {
		return nil
	}
	return proto.Clone(state.record.GetEnforcementManifest()).(*apipb.AllocationEnforcementManifest)
}

func (h *Controller) EnforcementManifest(allocationID string) *apipb.AllocationEnforcementManifest {
	allocationID = strings.TrimSpace(allocationID)
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	if state != nil && state.record != nil && state.record.GetEnforcementManifest() != nil {
		manifest := proto.Clone(state.record.GetEnforcementManifest()).(*apipb.AllocationEnforcementManifest)
		h.stateMu.RUnlock()
		return manifest
	}
	h.stateMu.RUnlock()
	if allocationID == "" || h.store == nil {
		return nil
	}
	var record apipb.AllocationState
	if err := h.store.GetRecord(config.AllocationStateBucket, allocationID, &record); err != nil || record.GetAllocationID() != allocationID || record.GetEnforcementManifest() == nil {
		return nil
	}
	return proto.Clone(record.GetEnforcementManifest()).(*apipb.AllocationEnforcementManifest)
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
	}
	sort.Slice(out, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(out[i].GetKey())
		right, _ := capabilitycontract.KeyID(out[j].GetKey())
		return left < right
	})
	return out, nil
}

func capabilityConditionKeysEqualDependencies(dependencies []*capabilityv1.CapabilityRequirement, conditions []*capabilityv1.CapabilityCondition) bool {
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

func cloneCapabilityRequirements(in []*capabilityv1.CapabilityRequirement) []*capabilityv1.CapabilityRequirement {
	out := make([]*capabilityv1.CapabilityRequirement, 0, len(in))
	for _, dependency := range in {
		if dependency != nil {
			out = append(out, proto.Clone(dependency).(*capabilityv1.CapabilityRequirement))
		}
	}
	return out
}

func (h *Controller) persistAllocationRecord(record *apipb.AllocationState) error {
	if record == nil || strings.TrimSpace(record.GetAllocationID()) == "" {
		return errors.New("allocation state requires an allocation id")
	}
	// Node-local sessions and conformance probes have no admitted node and keep
	// this aggregate in memory only.
	if strings.TrimSpace(record.GetNodeID()) == "" {
		return nil
	}
	if allocationRecordEmpty(record) {
		return h.store.DeleteRecord(config.AllocationStateBucket, record.GetAllocationID())
	}
	return h.store.PutRecord(config.AllocationStateBucket, record.GetAllocationID(), record)
}

func (h *Controller) rememberContainerRuntime(allocationID string, runtime *environmentcache.PreparedEnvironment) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation id is required")
	}
	if runtime == nil || runtime.ResolvedEnvironment() == nil {
		return errors.New("allocation environment template is required")
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
	desired.Environment = proto.Clone(runtime.ResolvedEnvironment()).(*apipb.ResolvedEnvironment)
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

func (h *Controller) rememberImageMountRoots(allocationID string, roots []*environmentcache.RootFS, mounts []*apipb.ImageMount) error {
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
	roots := append([]*environmentcache.RootFS(nil), state.imageMountRoots...)
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
		if allocationRecordEmpty(state.record) && state.runtime == nil {
			delete(h.allocationStates, allocationID)
		}
	}
	h.stateMu.Unlock()
	releaseImageMountRoots(roots)
}

func (h *Controller) releaseAllocationState(allocationID string, persistedRecovery bool) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil
	}
	unlock := h.recordMutationLocks.Lock(allocationID)
	defer unlock()
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	h.stateMu.RUnlock()
	if persistedRecovery || (state != nil && strings.TrimSpace(state.record.GetNodeID()) != "") {
		if err := h.store.DeleteRecord(config.AllocationStateBucket, allocationID); err != nil {
			return fmt.Errorf("delete allocation state: %w", err)
		}
	}
	h.stateMu.Lock()
	state = h.allocationStates[allocationID]
	delete(h.allocationStates, allocationID)
	h.stateMu.Unlock()
	if state == nil {
		return nil
	}
	if state.runtime != nil {
		state.runtime.DecRef()
	}
	releaseImageMountRoots(state.imageMountRoots)
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
	if record.GetEnvironment() == nil {
		recoveryErr = errors.Join(recoveryErr, errors.New("active allocation has no environment template"))
	} else {
		rootfsConfig, err := environmentcache.RootfsConfigFromResolvedEnvironment(record.GetEnvironment())
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
		} else {
			result, err := h.environmentCache.PrepareEnvironment(context.Background(), record.GetEnvironment(), rootfsConfig)
			if err != nil {
				recoveryErr = errors.Join(recoveryErr, err)
			} else {
				state.runtime = result.Environment
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
	enforcementVerified, err := classifyRecoveryRecord(record, now)
	if err != nil {
		return err
	}
	if !enforcementVerified {
		return errors.New("active allocation is missing its verified enforcement manifest")
	}
	return nil
}

func classifyRecoveryRecord(record *apipb.AllocationState, now time.Time) (bool, error) {
	if record == nil {
		return false, errors.New("allocation recovery record is required")
	}
	dependencies := record.GetCapabilityRequirements()
	if err := capabilitycontract.ValidateRequirements(dependencies); err != nil {
		return false, fmt.Errorf("validate recovered capability dependencies: %w", err)
	}
	if !validStartRequestDigest(record.GetAllocationRequestDigest()) {
		return false, errors.New("allocation create intent is missing its canonical request digest")
	}
	if strings.TrimSpace(record.GetNodeID()) != "" && record.GetExecutionLeaseExpiresAtUnixNano() <= 0 {
		return false, errors.New("control-plane allocation is missing its execution lease deadline")
	}
	manifest := record.GetEnforcementManifest()
	if manifest == nil {
		if record.GetCapabilityReconcile() != nil {
			return false, errors.New("unverified allocation create intent contains capability reconcile state")
		}
		return false, nil
	}
	if err := runtimecontract.ValidateEnforcementManifest(manifest, ""); err != nil {
		return false, fmt.Errorf("validate recovered enforcement manifest: %w", err)
	}
	if _, err := RequiredEnforcementKeys(manifest, dependencies); err != nil {
		return false, fmt.Errorf("validate recovered enforcement requirements: %w", err)
	}
	if err := validateCapabilityReconcileState(record.GetCapabilityReconcile(), dependencies, now); err != nil {
		return false, fmt.Errorf("validate recovered capability reconcile state: %w", err)
	}
	return true, nil
}

func validateCapabilityReconcileState(state *apipb.AllocationCapabilityReconcileState, _ []*capabilityv1.CapabilityRequirement, _ time.Time) error {
	if state == nil {
		return nil
	}
	if len(state.GetLastError()) > capabilitycontract.MaxReasonBytes {
		return errors.New("capability reconcile error exceeds its bounded payload")
	}
	if state.GetPendingIntentSequence() < 0 {
		return errors.New("pending capability reconcile intent sequence cannot be negative")
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
	return nil
}

func (h *Controller) acquireRecoveredImageRoot(imageURL string) (*environmentcache.RootFS, error) {
	config, err := h.environmentCache.ResolveRootfsConfig(environmentcache.RootfsConfig{SrcType: apipb.RootfsSrcType_IMAGE, ImageUrl: imageURL})
	if err != nil {
		return nil, err
	}
	rootfs, err := h.environmentCache.GetRootfs(config)
	if err != nil {
		return nil, err
	}
	if err := rootfs.IncActiveRef(); err != nil {
		return nil, err
	}
	return rootfs, nil
}
