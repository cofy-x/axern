package nodecapability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const recoveryConfirmationDelay = 5 * time.Second

type Provider interface {
	Provider() capabilityv1.CapabilityProvider
	ExpectedKeys() []*capabilityv1.CapabilityKey
	Observe(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error)
}

// Deriver owns composite capabilities whose truth is computed exclusively
// from observations produced in the same refresh generation.
type Deriver interface {
	Provider
	Derive(context.Context, time.Time, map[string]*capabilityv1.CapabilityObservation) ([]*capabilityv1.CapabilityObservation, error)
}

type recoveryState struct {
	hadFailure             bool
	firstSuccess           time.Time
	lastSuccessObservation time.Time
	successes              int
}

type Transition struct {
	Key      *capabilityv1.CapabilityKey
	Previous *capabilityv1.CapabilityObservation
	Current  *capabilityv1.CapabilityObservation
}

type TransitionHandler func(context.Context, []*Transition)

type MetricsObserver interface {
	RecordProviderProbe(provider capabilityv1.CapabilityProvider, result string, duration time.Duration)
	RecordSnapshot(snapshot *capabilityv1.CapabilitySnapshot)
	RecordTransitions(transitions []*Transition)
}

type Manager struct {
	refreshMu          sync.Mutex
	mu                 sync.RWMutex
	providers          []Provider
	ownerByKey         map[string]capabilityv1.CapabilityProvider
	recoveryByKey      map[string]recoveryState
	nodeInstanceID     string
	sequence           int64
	snapshot           *capabilityv1.CapabilitySnapshot
	initialized        bool
	transitionHandlers []TransitionHandler
	metrics            MetricsObserver
}

func (m *Manager) SetMetricsObserver(observer MetricsObserver) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.metrics = observer
	m.mu.Unlock()
}

func (m *Manager) Subscribe(handler TransitionHandler) {
	if m == nil || handler == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitionHandlers = append(m.transitionHandlers, handler)
}

func NewManager(providers ...Provider) (*Manager, error) {
	manager := &Manager{
		providers:      append([]Provider(nil), providers...),
		ownerByKey:     make(map[string]capabilityv1.CapabilityProvider),
		recoveryByKey:  make(map[string]recoveryState),
		nodeInstanceID: uuid.NewString(),
	}
	for _, provider := range manager.providers {
		if provider == nil || provider.Provider() == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_UNSPECIFIED {
			return nil, fmt.Errorf("capability provider and typed provider name are required")
		}
		for _, key := range provider.ExpectedKeys() {
			id, err := capabilitycontract.KeyID(key)
			if err != nil {
				return nil, fmt.Errorf("provider %s expected key: %w", provider.Provider(), err)
			}
			if owner, duplicate := manager.ownerByKey[id]; duplicate {
				return nil, fmt.Errorf("capability %q is owned by both %s and %s", id, owner, provider.Provider())
			}
			expectedProvider, _, err := capabilitycontract.ObservationOwner(key)
			if err != nil {
				return nil, fmt.Errorf("provider %s expected key %q: %w", provider.Provider(), id, err)
			}
			if provider.Provider() != expectedProvider {
				return nil, fmt.Errorf("capability %q is catalog-owned by %s, not %s", id, expectedProvider, provider.Provider())
			}
			manager.ownerByKey[id] = provider.Provider()
		}
	}
	return manager, nil
}

func (m *Manager) Ready() bool {
	if m == nil {
		return false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialized
}

func (m *Manager) Snapshot() *capabilityv1.CapabilitySnapshot {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.snapshot == nil {
		return nil
	}
	return proto.Clone(m.snapshot).(*capabilityv1.CapabilitySnapshot)
}

// AdmitDependencies binds one allocation to the latest complete evidence in
// a single atomic snapshot. A newer valid evidence identity may replace the
// placement evidence; callers must persist the returned dependencies before
// creating runtime side effects.
func (m *Manager) AdmitDependencies(dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	snapshot := m.Snapshot()
	if snapshot == nil || !m.Ready() {
		return nil, nil, fmt.Errorf("capability manager is warming")
	}
	keys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	for _, dependency := range dependencies {
		id, err := capabilitycontract.KeyID(dependency.GetKey())
		if err != nil {
			return nil, nil, err
		}
		expectedPolicy, err := capabilitycontract.LossPolicy(dependency.GetKey())
		if err != nil {
			return nil, nil, err
		}
		if dependency.GetLossPolicy() != expectedPolicy {
			return nil, nil, fmt.Errorf("capability %q loss policy is not catalog-owned policy %s", id, expectedPolicy)
		}
		keys = append(keys, capabilitycontract.CloneKey(dependency.GetKey()))
	}
	admitted, err := capabilitycontract.ResolveDependencies(snapshot, keys, now)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve dependencies from snapshot %q: %w", snapshot.GetSnapshotID(), err)
	}
	selectedByKey := make(map[string]*capabilityv1.CapabilityEvidence, len(dependencies))
	for _, dependency := range dependencies {
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		selectedByKey[id] = dependency.GetSelectedEvidence()
	}
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(admitted))
	for _, dependency := range admitted {
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		conditions = append(conditions, &capabilityv1.CapabilityCondition{
			Key:        proto.Clone(dependency.GetKey()).(*capabilityv1.CapabilityKey),
			State:      capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			Message:    evidenceReplacementMessage(selectedByKey[id], dependency.GetSelectedEvidence()),
			ObservedAt: timestamppb.New(now.UTC()),
			Evidence:   proto.Clone(dependency.GetSelectedEvidence()).(*capabilityv1.CapabilityEvidence),
		})
	}
	return admitted, conditions, nil
}

func (m *Manager) VerifyDependencies(dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityCondition, error) {
	_, conditions, err := m.AdmitDependencies(dependencies, now)
	return conditions, err
}

func evidenceReplacementMessage(selected, admitted *capabilityv1.CapabilityEvidence) string {
	if selected.GetEvidenceID() == admitted.GetEvidenceID() {
		return "placement evidence remains valid"
	}
	return fmt.Sprintf("placement evidence %s replaced by current evidence %s", selected.GetEvidenceID(), admitted.GetEvidenceID())
}

func (m *Manager) Refresh(ctx context.Context, now time.Time) (*capabilityv1.CapabilitySnapshot, error) {
	if m == nil {
		return nil, fmt.Errorf("capability manager is required")
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	now = now.UTC()
	m.mu.RLock()
	previousCollectedAt := time.Time{}
	if m.snapshot != nil && m.snapshot.GetCollectedAt() != nil {
		previousCollectedAt = m.snapshot.GetCollectedAt().AsTime().UTC()
	}
	recovery := cloneRecoveryState(m.recoveryByKey)
	m.mu.RUnlock()
	if !previousCollectedAt.IsZero() && now.Before(previousCollectedAt) {
		return nil, fmt.Errorf("capability refresh time %s precedes published snapshot time %s", now.Format(time.RFC3339Nano), previousCollectedAt.Format(time.RFC3339Nano))
	}
	observations := make(map[string]*capabilityv1.CapabilityObservation, len(m.ownerByKey))
	for _, provider := range m.providers {
		if _, derived := provider.(Deriver); derived {
			continue
		}
		started := time.Now()
		items, err := provider.Observe(ctx, now)
		m.recordProviderProbe(provider.Provider(), err, time.Since(started))
		for _, item := range items {
			id, keyErr := capabilitycontract.KeyID(item.GetKey())
			if keyErr != nil {
				return nil, fmt.Errorf("provider %s observation: %w", provider.Provider(), keyErr)
			}
			if owner, ok := m.ownerByKey[id]; !ok || owner != provider.Provider() {
				return nil, fmt.Errorf("provider %s does not own capability %q", provider.Provider(), id)
			}
			if _, duplicate := observations[id]; duplicate {
				return nil, fmt.Errorf("provider %s returned duplicate capability %q", provider.Provider(), id)
			}
			observation := normalizeObservation(item, provider.Provider(), now)
			if err := validateManagedObservation(observation); err != nil {
				return nil, fmt.Errorf("provider %s observation %q: %w", provider.Provider(), id, err)
			}
			observations[id] = observation
		}
		for _, key := range provider.ExpectedKeys() {
			id, _ := capabilitycontract.KeyID(key)
			if _, ok := observations[id]; ok {
				continue
			}
			reason := "provider did not return the expected observation"
			if err != nil {
				reason = err.Error()
			}
			observations[id] = unknownObservation(key, provider.Provider(), now, reason)
		}
	}
	// Recovery hysteresis is part of the effective base observation. Derived
	// providers must never see an optimistic raw success that the manager has
	// not yet confirmed, otherwise the resulting snapshot mixes incompatible
	// states from the same generation.
	for key, observation := range observations {
		applyRecoveryPolicy(recovery, key, observation, now)
	}
	for _, provider := range m.providers {
		deriver, derived := provider.(Deriver)
		if !derived {
			continue
		}
		started := time.Now()
		items, err := deriver.Derive(ctx, now, cloneObservationMap(observations))
		m.recordProviderProbe(provider.Provider(), err, time.Since(started))
		for _, item := range items {
			id, keyErr := capabilitycontract.KeyID(item.GetKey())
			if keyErr != nil {
				return nil, fmt.Errorf("provider %s observation: %w", provider.Provider(), keyErr)
			}
			if owner, ok := m.ownerByKey[id]; !ok || owner != provider.Provider() {
				return nil, fmt.Errorf("provider %s does not own capability %q", provider.Provider(), id)
			}
			if _, duplicate := observations[id]; duplicate {
				return nil, fmt.Errorf("provider %s returned duplicate capability %q", provider.Provider(), id)
			}
			observation := normalizeObservation(item, provider.Provider(), now)
			if err := validateManagedObservation(observation); err != nil {
				return nil, fmt.Errorf("provider %s observation %q: %w", provider.Provider(), id, err)
			}
			observations[id] = observation
		}
		for _, key := range provider.ExpectedKeys() {
			id, _ := capabilitycontract.KeyID(key)
			if _, ok := observations[id]; ok {
				continue
			}
			reason := "derived provider did not return the expected observation"
			if err != nil {
				reason = err.Error()
			}
			observation := unknownObservation(key, provider.Provider(), now, reason)
			observation.Dependencies = dependencyEvidenceReferences(key, observations)
			observations[id] = observation
		}
	}

	m.mu.Lock()
	previous := m.snapshot
	ordered := make([]*capabilityv1.CapabilityObservation, 0, len(observations))
	keys := make([]string, 0, len(observations))
	for key := range observations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ordered = append(ordered, observations[key])
	}
	m.sequence++
	candidate := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: m.nodeInstanceID,
		Sequence:       m.sequence,
		SnapshotID:     uuid.NewString(),
		CollectedAt:    timestamppb.New(now),
		Observations:   ordered,
	}
	if err := capabilitycontract.ValidateSnapshot(candidate, now); err != nil {
		m.sequence--
		m.mu.Unlock()
		return nil, fmt.Errorf("validate collected capability snapshot: %w", err)
	}
	m.recoveryByKey = recovery
	m.snapshot = candidate
	m.initialized = true
	current := proto.Clone(m.snapshot).(*capabilityv1.CapabilitySnapshot)
	transitions := snapshotTransitions(previous, current)
	handlers := append([]TransitionHandler(nil), m.transitionHandlers...)
	metrics := m.metrics
	m.mu.Unlock()
	if metrics != nil {
		metrics.RecordSnapshot(current)
		metrics.RecordTransitions(transitions)
	}
	if len(transitions) > 0 {
		for _, handler := range handlers {
			handler(ctx, transitions)
		}
	}
	// Provider failures are capability facts, not collection failures. They are
	// represented by complete UNKNOWN observations above so inventory can
	// publish the atomic snapshot while keeping unrelated capabilities usable.
	// A non-nil error is reserved for failures that prevent snapshot creation.
	return current, nil
}

func dependencyEvidenceReferences(key *capabilityv1.CapabilityKey, observations map[string]*capabilityv1.CapabilityObservation) []*capabilityv1.CapabilityEvidenceReference {
	if key == nil || key.GetExtension() != nil {
		return nil
	}
	keys, err := capabilitycontract.PlatformDependencyKeys(key.GetPlatform())
	if err != nil {
		return nil
	}
	references := make([]*capabilityv1.CapabilityEvidenceReference, 0, len(keys))
	for _, dependencyKey := range keys {
		id, err := capabilitycontract.KeyID(dependencyKey)
		if err != nil {
			continue
		}
		observation := observations[id]
		if observation == nil || observation.GetEvidence() == nil {
			continue
		}
		references = append(references, &capabilityv1.CapabilityEvidenceReference{
			Key:        capabilitycontract.CloneKey(dependencyKey),
			EvidenceID: observation.GetEvidence().GetEvidenceID(),
			Evidence:   proto.Clone(observation.GetEvidence()).(*capabilityv1.CapabilityEvidence),
		})
	}
	return references
}

func (m *Manager) recordProviderProbe(provider capabilityv1.CapabilityProvider, err error, duration time.Duration) {
	m.mu.RLock()
	metrics := m.metrics
	m.mu.RUnlock()
	if metrics == nil {
		return
	}
	result := "ok"
	if err != nil {
		result = "error"
	}
	metrics.RecordProviderProbe(provider, result, duration)
}

func snapshotTransitions(previous, current *capabilityv1.CapabilitySnapshot) []*Transition {
	if previous == nil {
		return nil
	}
	old := make(map[string]*capabilityv1.CapabilityObservation, len(previous.GetObservations()))
	for _, observation := range previous.GetObservations() {
		id, err := capabilitycontract.KeyID(observation.GetKey())
		if err == nil {
			old[id] = observation
		}
	}
	var transitions []*Transition
	for _, observation := range current.GetObservations() {
		id, err := capabilitycontract.KeyID(observation.GetKey())
		if err != nil {
			continue
		}
		previousObservation := old[id]
		if previousObservation.GetState() == observation.GetState() && previousObservation.GetEvidence().GetEvidenceID() == observation.GetEvidence().GetEvidenceID() {
			continue
		}
		transitions = append(transitions, &Transition{
			Key:      proto.Clone(observation.GetKey()).(*capabilityv1.CapabilityKey),
			Previous: cloneObservation(previousObservation), Current: cloneObservation(observation),
		})
	}
	return transitions
}

func cloneObservation(in *capabilityv1.CapabilityObservation) *capabilityv1.CapabilityObservation {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*capabilityv1.CapabilityObservation)
}

func cloneObservationMap(in map[string]*capabilityv1.CapabilityObservation) map[string]*capabilityv1.CapabilityObservation {
	out := make(map[string]*capabilityv1.CapabilityObservation, len(in))
	for key, observation := range in {
		out[key] = proto.Clone(observation).(*capabilityv1.CapabilityObservation)
	}
	return out
}

func applyRecoveryPolicy(recovery map[string]recoveryState, key string, observation *capabilityv1.CapabilityObservation, now time.Time) {
	state := recovery[key]
	if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		state.hadFailure = true
		state.firstSuccess = time.Time{}
		state.lastSuccessObservation = time.Time{}
		state.successes = 0
		recovery[key] = state
		return
	}
	if !state.hadFailure {
		return
	}
	observedAt := observation.GetObservedAt().AsTime().UTC()
	if state.successes == 0 {
		state.firstSuccess = now
		state.lastSuccessObservation = observedAt
		state.successes = 1
	} else if now.Sub(state.firstSuccess) >= recoveryConfirmationDelay && observedAt.After(state.lastSuccessObservation) {
		state.lastSuccessObservation = observedAt
		state.successes++
	}
	if state.successes < 2 {
		observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED
		observation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_RECOVERY_PENDING
		observation.Reason = "capability recovery requires a second successful observation"
	} else {
		delete(recovery, key)
		return
	}
	recovery[key] = state
}

func cloneRecoveryState(in map[string]recoveryState) map[string]recoveryState {
	out := make(map[string]recoveryState, len(in))
	for key, state := range in {
		out[key] = state
	}
	return out
}

func normalizeObservation(in *capabilityv1.CapabilityObservation, provider capabilityv1.CapabilityProvider, now time.Time) *capabilityv1.CapabilityObservation {
	out := &capabilityv1.CapabilityObservation{}
	if in != nil {
		out = proto.Clone(in).(*capabilityv1.CapabilityObservation)
	}
	out.Provider = provider
	if out.ObservedAt == nil {
		out.ObservedAt = timestamppb.New(now)
	}
	if out.State == capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED {
		out.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
	}
	if out.Evidence == nil {
		out.Evidence = &capabilityv1.CapabilityEvidence{}
	}
	if out.Evidence.EvidenceID == "" {
		out.Evidence.EvidenceID = evidenceID(out)
	}
	return out
}

func unknownObservation(key *capabilityv1.CapabilityKey, provider capabilityv1.CapabilityProvider, now time.Time, reason string) *capabilityv1.CapabilityObservation {
	_, scope, _ := capabilitycontract.ObservationOwner(key)
	return normalizeObservation(&capabilityv1.CapabilityObservation{
		Key:           proto.Clone(key).(*capabilityv1.CapabilityKey),
		State:         capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN,
		ValidityScope: scope,
		ObservedAt:    timestamppb.New(now),
		ValidUntil:    timestamppb.New(now),
		ReasonCode:    capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR,
		Reason:        reason,
	}, provider, now)
}

func validateManagedObservation(observation *capabilityv1.CapabilityObservation) error {
	expectedProvider, expectedScope, err := capabilitycontract.ObservationOwner(observation.GetKey())
	if err != nil {
		return err
	}
	if observation.GetProvider() != expectedProvider || observation.GetValidityScope() != expectedScope {
		return fmt.Errorf("catalog requires provider %s and scope %s", expectedProvider, expectedScope)
	}
	return nil
}

func evidenceID(observation *capabilityv1.CapabilityObservation) string {
	copy := proto.Clone(observation).(*capabilityv1.CapabilityObservation)
	copy.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED
	copy.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_UNSPECIFIED
	copy.Reason = ""
	copy.ObservedAt = nil
	copy.ValidUntil = nil
	if copy.Evidence != nil {
		copy.Evidence.EvidenceID = ""
	}
	payload, _ := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(copy)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
