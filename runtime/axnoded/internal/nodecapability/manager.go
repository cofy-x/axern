package nodecapability

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	recoveryConfirmationDelay = 5 * time.Second
	providerHealthInterval    = 5 * time.Second
)

type Provider interface {
	Provider() capabilityv1.CapabilityProvider
	ExpectedKeys() []*capabilityv1.CapabilityKey
	Observe(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error)
}

// Deriver owns composite capabilities whose truth is computed exclusively
// from the latest base batches selected for one publication.
type Deriver interface {
	Provider
	Derive(context.Context, time.Time, map[string]*capabilityv1.CapabilityObservation) ([]*capabilityv1.CapabilityObservation, error)
}

// ObservationBatch is one provider's atomic publication. Observations from a
// failed or malformed batch are never mixed with observations from an older
// successful batch.
type ObservationBatch struct {
	Provider     capabilityv1.CapabilityProvider
	SampledAt    time.Time
	CompletedAt  time.Time
	Observations []*capabilityv1.CapabilityObservation
}

type recoveryState struct {
	hadFailure       bool
	firstSuccess     time.Time
	lastSuccessProof string
	successes        int
}

type providerSlot struct {
	id       string
	provider Provider
	keys     []*capabilityv1.CapabilityKey
	sampleMu sync.Mutex
}

type Transition struct {
	Generation int64
	Key        *capabilityv1.CapabilityKey
	Previous   *capabilityv1.CapabilityObservation
	Current    *capabilityv1.CapabilityObservation
}

type TransitionHandler func(context.Context, []*Transition)

type MetricsObserver interface {
	RecordProviderProbe(provider capabilityv1.CapabilityProvider, result string, duration time.Duration)
	RecordSnapshot(snapshot *capabilityv1.CapabilitySnapshot)
	RecordTransitions(transitions []*Transition)
}

type publicationDelivery struct {
	ctx         context.Context
	snapshot    *capabilityv1.CapabilitySnapshot
	transitions []*Transition
	handlers    []TransitionHandler
	metrics     MetricsObserver
}

type Manager struct {
	mu                 sync.RWMutex
	publishMu          sync.Mutex
	deliveryMu         sync.Mutex
	deliveryQueue      []publicationDelivery
	deliveryWake       chan struct{}
	deliveryStarted    bool
	baseSlots          []*providerSlot
	derivers           []Deriver
	ownerByKey         map[string]capabilityv1.CapabilityProvider
	batchBySlot        map[string]*ObservationBatch
	recoveryByKey      map[string]recoveryState
	nodeInstanceID     string
	sequence           int64
	snapshot           *capabilityv1.CapabilitySnapshot
	initialized        bool
	transitionHandlers []TransitionHandler
	metrics            MetricsObserver
	runtimeProbeSerial chan struct{}
	startOnce          sync.Once
}

func NewManager(providers ...Provider) (*Manager, error) {
	manager := &Manager{
		ownerByKey:         make(map[string]capabilityv1.CapabilityProvider),
		batchBySlot:        make(map[string]*ObservationBatch),
		recoveryByKey:      make(map[string]recoveryState),
		nodeInstanceID:     uuid.NewString(),
		runtimeProbeSerial: make(chan struct{}, 1),
		deliveryWake:       make(chan struct{}, 1),
	}
	manager.runtimeProbeSerial <- struct{}{}
	for index, provider := range providers {
		if provider == nil || provider.Provider() == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_UNSPECIFIED {
			return nil, fmt.Errorf("capability provider and typed provider name are required")
		}
		keys := provider.ExpectedKeys()
		if len(keys) == 0 {
			return nil, fmt.Errorf("capability provider %s owns no keys", provider.Provider())
		}
		for _, key := range keys {
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
		if deriver, ok := provider.(Deriver); ok {
			manager.derivers = append(manager.derivers, deriver)
			continue
		}
		manager.baseSlots = append(manager.baseSlots, &providerSlot{id: fmt.Sprintf("%d/%d", provider.Provider(), index), provider: provider, keys: cloneKeys(keys)})
	}
	return manager, nil
}

// ValidateCatalogProviderCoverage proves that a production provider set owns
// every catalog-defined platform key exactly once. Manager unit tests may use
// intentionally partial provider sets, but service startup must call this
// before constructing the live manager so an omitted provider cannot be
// represented as an ambiguous absent observation.
func ValidateCatalogProviderCoverage(providers ...Provider) error {
	owners := make(map[string]capabilityv1.CapabilityProvider)
	for _, provider := range providers {
		if provider == nil {
			return fmt.Errorf("capability provider is required")
		}
		for _, key := range provider.ExpectedKeys() {
			if key.GetExtension() != nil {
				continue
			}
			id, err := capabilitycontract.KeyID(key)
			if err != nil {
				return fmt.Errorf("provider %s expected key: %w", provider.Provider(), err)
			}
			if owner, duplicate := owners[id]; duplicate {
				return fmt.Errorf("platform capability %q is owned by both %s and %s", id, owner, provider.Provider())
			}
			owners[id] = provider.Provider()
		}
	}
	for _, definition := range capabilitycontract.PlatformDefinitions() {
		key := capabilitycontract.PlatformKey(definition.Key)
		id, _ := capabilitycontract.KeyID(key)
		owner, ok := owners[id]
		if !ok {
			return fmt.Errorf("platform capability %q has no registered provider", id)
		}
		if owner != definition.Provider {
			return fmt.Errorf("platform capability %q is catalog-owned by %s, not %s", id, definition.Provider, owner)
		}
	}
	return nil
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
	m.transitionHandlers = append(m.transitionHandlers, handler)
	m.mu.Unlock()
}

// Start launches one scheduler per base provider. Runtime self-tests share a
// serial lane, while network and mount providers keep publishing independently.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	m.startOnce.Do(func() {
		m.startDeliveryWorker(ctx)
		if len(m.baseSlots) == 0 {
			_, _ = m.publish(ctx, time.Now().UTC())
			return
		}
		for _, slot := range m.baseSlots {
			slot := slot
			go m.runProvider(ctx, slot)
		}
	})
}

// startDeliveryWorker separates snapshot publication from transition side
// effects. Providers can keep publishing fresh health while one handler is
// reconciling many allocations. The single worker preserves publication order.
func (m *Manager) startDeliveryWorker(ctx context.Context) {
	m.deliveryMu.Lock()
	if m.deliveryStarted {
		m.deliveryMu.Unlock()
		return
	}
	m.deliveryStarted = true
	m.deliveryMu.Unlock()
	go m.runDeliveryWorker(ctx)
}

func (m *Manager) runDeliveryWorker(ctx context.Context) {
	for {
		for {
			delivery, ok := m.takeDelivery()
			if !ok {
				break
			}
			deliverPublication(delivery)
		}
		select {
		case <-ctx.Done():
			return
		case <-m.deliveryWake:
		}
	}
}

func (m *Manager) takeDelivery() (publicationDelivery, bool) {
	m.deliveryMu.Lock()
	defer m.deliveryMu.Unlock()
	if len(m.deliveryQueue) == 0 {
		return publicationDelivery{}, false
	}
	delivery := m.deliveryQueue[0]
	m.deliveryQueue[0] = publicationDelivery{}
	m.deliveryQueue = m.deliveryQueue[1:]
	return delivery, true
}

func (m *Manager) enqueueDelivery(delivery publicationDelivery) bool {
	m.deliveryMu.Lock()
	if !m.deliveryStarted {
		m.deliveryMu.Unlock()
		return false
	}
	// Periodic provider publications commonly advance only snapshot freshness.
	// While a transition handler is busy, retaining every intermediate
	// no-transition snapshot would turn a bounded callback delay into an
	// unbounded memory queue. Only the newest such snapshot is observable; real
	// transitions remain separate ordered deliveries and are never coalesced.
	if len(delivery.transitions) == 0 && len(m.deliveryQueue) > 0 && len(m.deliveryQueue[len(m.deliveryQueue)-1].transitions) == 0 {
		m.deliveryQueue[len(m.deliveryQueue)-1] = delivery
		m.deliveryMu.Unlock()
		return true
	}
	m.deliveryQueue = append(m.deliveryQueue, delivery)
	m.deliveryMu.Unlock()
	select {
	case m.deliveryWake <- struct{}{}:
	default:
	}
	return true
}

func (m *Manager) runProvider(ctx context.Context, slot *providerSlot) {
	backoff := providerHealthInterval
	for {
		m.sampleAndPublish(ctx, slot, time.Now().UTC(), true)
		if isStaticProvider(slot.provider.Provider()) {
			return
		}
		delay := providerHealthInterval
		if slot.provider.Provider() == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP {
			if m.providerBatchAvailable(slot.id) {
				return
			}
			delay = backoff
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if slot.provider.Provider() == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP {
				backoff = min(backoff*2, 5*time.Minute)
			}
		}
	}
}

func (m *Manager) providerBatchAvailable(slotID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	batch := m.batchBySlot[slotID]
	if batch == nil || len(batch.Observations) == 0 {
		return false
	}
	for _, observation := range batch.Observations {
		if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			return false
		}
	}
	return true
}

func isStaticProvider(provider capabilityv1.CapabilityProvider) bool {
	return provider == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG
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

// Refresh is a deterministic collection barrier used by tests and explicit
// readiness probes. Production refresh uses Start so a slow provider cannot
// delay unrelated batches.
func (m *Manager) Refresh(ctx context.Context, now time.Time) (*capabilityv1.CapabilitySnapshot, error) {
	if m == nil {
		return nil, fmt.Errorf("capability manager is required")
	}
	now = now.UTC()
	m.mu.RLock()
	if m.snapshot != nil && m.snapshot.GetCollectedAt() != nil && now.Before(m.snapshot.GetCollectedAt().AsTime()) {
		previous := m.snapshot.GetCollectedAt().AsTime()
		m.mu.RUnlock()
		return nil, fmt.Errorf("capability refresh time %s precedes published snapshot time %s", now.Format(time.RFC3339Nano), previous.Format(time.RFC3339Nano))
	}
	m.mu.RUnlock()
	var wait sync.WaitGroup
	for _, slot := range m.baseSlots {
		wait.Add(1)
		go func(slot *providerSlot) {
			defer wait.Done()
			m.sample(ctx, slot, now, false)
		}(slot)
	}
	wait.Wait()
	return m.publish(ctx, now)
}

func (m *Manager) sampleAndPublish(ctx context.Context, slot *providerSlot, sampledAt time.Time, realtime bool) {
	m.sample(ctx, slot, sampledAt, realtime)
	publishedAt := sampledAt
	if realtime {
		publishedAt = time.Now().UTC()
	}
	_, _ = m.publish(ctx, publishedAt)
}

func (m *Manager) sample(ctx context.Context, slot *providerSlot, sampledAt time.Time, realtime bool) {
	slot.sampleMu.Lock()
	defer slot.sampleMu.Unlock()
	started := time.Now()
	items, probeErr := m.observeSafely(ctx, slot.provider, sampledAt)
	completedAt := sampledAt
	if realtime {
		completedAt = time.Now().UTC()
	}
	batch, batchErr := m.normalizeBatch(slot, sampledAt, completedAt, items, probeErr)
	m.recordProviderProbe(slot.provider.Provider(), firstError(probeErr, batchErr), time.Since(started))
	m.mu.Lock()
	m.batchBySlot[slot.id] = batch
	m.mu.Unlock()
}

func (m *Manager) observeSafely(ctx context.Context, provider Provider, now time.Time) (items []*capabilityv1.CapabilityObservation, err error) {
	if isRuntimeProvider(provider.Provider()) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.runtimeProbeSerial:
			defer func() { m.runtimeProbeSerial <- struct{}{} }()
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("provider panic: %v", recovered)
			items = nil
		}
	}()
	return provider.Observe(ctx, now)
}

func isRuntimeProvider(provider capabilityv1.CapabilityProvider) bool {
	return provider == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST || provider == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST
}

func (m *Manager) normalizeBatch(slot *providerSlot, sampledAt, completedAt time.Time, items []*capabilityv1.CapabilityObservation, probeErr error) (*ObservationBatch, error) {
	if probeErr != nil {
		return m.unknownBatch(slot, sampledAt, completedAt, probeErr.Error()), probeErr
	}
	expected := make(map[string]*capabilityv1.CapabilityKey, len(slot.keys))
	for _, key := range slot.keys {
		id, _ := capabilitycontract.KeyID(key)
		expected[id] = key
	}
	byKey := make(map[string]*capabilityv1.CapabilityObservation, len(items))
	for _, item := range items {
		id, err := capabilitycontract.KeyID(item.GetKey())
		if err != nil {
			return m.unknownBatch(slot, sampledAt, completedAt, err.Error()), err
		}
		if _, owned := expected[id]; !owned {
			err = fmt.Errorf("provider %s returned unowned key %q", slot.provider.Provider(), id)
			return m.unknownBatch(slot, sampledAt, completedAt, err.Error()), err
		}
		if _, duplicate := byKey[id]; duplicate {
			err = fmt.Errorf("provider %s returned duplicate key %q", slot.provider.Provider(), id)
			return m.unknownBatch(slot, sampledAt, completedAt, err.Error()), err
		}
		observation := normalizeObservation(item, slot.provider.Provider(), completedAt)
		byKey[id] = observation
	}
	if len(byKey) != len(expected) {
		err := fmt.Errorf("provider %s returned %d observations for %d owned keys", slot.provider.Provider(), len(byKey), len(expected))
		return m.unknownBatch(slot, sampledAt, completedAt, err.Error()), err
	}
	ordered := make([]*capabilityv1.CapabilityObservation, 0, len(byKey))
	ids := make([]string, 0, len(byKey))
	for id := range byKey {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ordered = append(ordered, byKey[id])
	}
	if err := validateBatch(slot.provider.Provider(), ordered, completedAt); err != nil {
		return m.unknownBatch(slot, sampledAt, completedAt, err.Error()), err
	}
	m.mu.Lock()
	for index, observation := range ordered {
		id, _ := capabilitycontract.KeyID(observation.GetKey())
		capabilitycontract.NormalizeObservation(observation)
		state := m.recoveryByKey[id]
		applyRecoveryPolicy(&state, observation, completedAt)
		if state.hadFailure {
			m.recoveryByKey[id] = state
		} else {
			delete(m.recoveryByKey, id)
		}
		capabilitycontract.NormalizeObservation(observation)
		ordered[index] = observation
	}
	m.mu.Unlock()
	return &ObservationBatch{Provider: slot.provider.Provider(), SampledAt: sampledAt, CompletedAt: completedAt, Observations: ordered}, nil
}

func validateBatch(provider capabilityv1.CapabilityProvider, observations []*capabilityv1.CapabilityObservation, now time.Time) error {
	snapshot := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "batch", Sequence: 1, SnapshotID: "batch", CollectedAt: timestamppb.New(now), Observations: observations}
	if err := capabilitycontract.ValidateSnapshot(snapshot, now); err != nil {
		return fmt.Errorf("malformed %s batch: %w", provider, err)
	}
	return nil
}

func (m *Manager) unknownBatch(slot *providerSlot, sampledAt, completedAt time.Time, reason string) *ObservationBatch {
	observations := make([]*capabilityv1.CapabilityObservation, 0, len(slot.keys))
	for _, key := range slot.keys {
		observations = append(observations, unknownObservation(key, slot.provider.Provider(), completedAt, reason))
	}
	m.applyRecoveryObservations(observations, completedAt)
	return &ObservationBatch{Provider: slot.provider.Provider(), SampledAt: sampledAt, CompletedAt: completedAt, Observations: observations}
}

func (m *Manager) publish(ctx context.Context, now time.Time) (*capabilityv1.CapabilitySnapshot, error) {
	// Provider schedulers publish independently, but snapshot sequence,
	// transition derivation, and ordered handler delivery form one serial
	// publication log. Without this lock, two providers can derive transitions
	// from the same previous snapshot and deliver generations out of order.
	m.publishMu.Lock()
	defer m.publishMu.Unlock()
	m.mu.Lock()
	previous := cloneSnapshot(m.snapshot)
	baseComplete := len(m.batchBySlot) == len(m.baseSlots)
	base := make(map[string]*capabilityv1.CapabilityObservation, len(m.ownerByKey))
	for _, batch := range m.batchBySlot {
		for _, item := range batch.Observations {
			observation := proto.Clone(item).(*capabilityv1.CapabilityObservation)
			if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && observation.GetValidUntil() != nil && !observation.GetValidUntil().AsTime().After(now) {
				id, _ := capabilitycontract.KeyID(observation.GetKey())
				state := m.recoveryByKey[id]
				state.hadFailure = true
				state.firstSuccess = time.Time{}
				state.lastSuccessProof = ""
				state.successes = 0
				m.recoveryByKey[id] = state
				observation = unknownObservation(observation.GetKey(), observation.GetProvider(), now, "capability observation expired before its provider refreshed")
				observation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED
				capabilitycontract.NormalizeObservation(observation)
			}
			id, _ := capabilitycontract.KeyID(observation.GetKey())
			base[id] = observation
		}
	}
	m.mu.Unlock()

	all := cloneObservationMap(base)
	for _, deriver := range m.derivers {
		if !baseComplete {
			for _, item := range warmingDerivedObservations(deriver, now) {
				id, _ := capabilitycontract.KeyID(item.GetKey())
				all[id] = item
			}
			continue
		}
		items, err := deriveSafely(ctx, deriver, now, cloneObservationMap(all))
		if err != nil {
			items = nil
		}
		normalized, normalizeErr := normalizeDerivedBatch(deriver, items, all, now, firstError(err, nil))
		if normalizeErr != nil {
			normalized, _ = normalizeDerivedBatch(deriver, nil, all, now, normalizeErr)
		}
		// Derived capabilities do not run an independent probe. Their base
		// observations have already satisfied the manager's two-sample recovery
		// policy, so applying the same debounce again would require a third and
		// fourth proof. Runtime-derived capabilities could otherwise remain
		// unavailable until the next 15-minute conformance probe.
		for _, item := range normalized {
			id, _ := capabilitycontract.KeyID(item.GetKey())
			all[id] = item
		}
	}

	ordered := make([]*capabilityv1.CapabilityObservation, 0, len(all))
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ordered = append(ordered, all[id])
	}

	m.mu.Lock()
	if m.snapshot != nil && m.snapshot.GetCollectedAt() != nil && now.Before(m.snapshot.GetCollectedAt().AsTime()) {
		m.mu.Unlock()
		return nil, fmt.Errorf("snapshot publication time moved backwards")
	}
	m.sequence++
	candidate := &capabilityv1.CapabilitySnapshot{NodeInstanceID: m.nodeInstanceID, Sequence: m.sequence, SnapshotID: uuid.NewString(), CollectedAt: timestamppb.New(now), Observations: ordered}
	if err := capabilitycontract.ValidateSnapshot(candidate, now); err != nil {
		m.sequence--
		m.mu.Unlock()
		return nil, fmt.Errorf("validate collected capability snapshot: %w", err)
	}
	m.snapshot = candidate
	m.initialized = len(m.batchBySlot) == len(m.baseSlots) && len(all) == len(m.ownerByKey)
	current := cloneSnapshot(candidate)
	transitions := snapshotTransitions(previous, current)
	handlers := append([]TransitionHandler(nil), m.transitionHandlers...)
	metrics := m.metrics
	m.mu.Unlock()
	// Publication ownership is manager-scoped, not request-scoped. A canceled
	// probe or refresh caller must not suppress the durable reconcile enqueue
	// performed by a transition handler after the snapshot has been committed.
	delivery := publicationDelivery{ctx: context.WithoutCancel(ctx), snapshot: current, transitions: transitions, handlers: handlers, metrics: metrics}
	if !m.enqueueDelivery(delivery) {
		deliverPublication(delivery)
	}
	return current, nil
}

func deliverPublication(delivery publicationDelivery) {
	if delivery.metrics != nil {
		callDeliverySafely("metrics", func() {
			delivery.metrics.RecordSnapshot(delivery.snapshot)
			delivery.metrics.RecordTransitions(delivery.transitions)
		})
	}
	if len(delivery.transitions) == 0 {
		return
	}
	for _, handler := range delivery.handlers {
		handler := handler
		callDeliverySafely("transition_handler", func() { handler(delivery.ctx, delivery.transitions) })
	}
}

func callDeliverySafely(kind string, deliver func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logrus.WithField("delivery", kind).WithField("panic", recovered).Error("capability publication delivery panicked")
		}
	}()
	deliver()
}

func warmingDerivedObservations(deriver Deriver, now time.Time) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(deriver.ExpectedKeys()))
	for _, key := range deriver.ExpectedKeys() {
		out = append(out, unknownObservation(key, deriver.Provider(), now, "base capability providers are warming"))
	}
	return out
}

// applyRecoveryObservations owns recovery hysteresis for every publication
// path, including probe errors, malformed batches, expiry, and derived
// failures. Keeping this in the manager prevents providers from bypassing the
// two-sample recovery contract by choosing a particular failure state.
func (m *Manager) applyRecoveryObservations(observations []*capabilityv1.CapabilityObservation, completedAt time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, observation := range observations {
		id, err := capabilitycontract.KeyID(observation.GetKey())
		if err != nil {
			continue
		}
		state := m.recoveryByKey[id]
		applyRecoveryPolicy(&state, observation, completedAt)
		if state.hadFailure {
			m.recoveryByKey[id] = state
		} else {
			delete(m.recoveryByKey, id)
		}
		capabilitycontract.NormalizeObservation(observation)
	}
}

func deriveSafely(ctx context.Context, deriver Deriver, now time.Time, observations map[string]*capabilityv1.CapabilityObservation) (items []*capabilityv1.CapabilityObservation, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("derived provider panic: %v", recovered)
			items = nil
		}
	}()
	return deriver.Derive(ctx, now, observations)
}

func normalizeDerivedBatch(deriver Deriver, items []*capabilityv1.CapabilityObservation, base map[string]*capabilityv1.CapabilityObservation, now time.Time, deriveErr error) ([]*capabilityv1.CapabilityObservation, error) {
	expected := make(map[string]*capabilityv1.CapabilityKey, len(deriver.ExpectedKeys()))
	for _, key := range deriver.ExpectedKeys() {
		id, _ := capabilitycontract.KeyID(key)
		expected[id] = key
	}
	if deriveErr != nil {
		return unknownDerived(expected, deriver.Provider(), now, deriveErr.Error()), nil
	}
	byKey := make(map[string]*capabilityv1.CapabilityObservation, len(items))
	for _, item := range items {
		id, err := capabilitycontract.KeyID(item.GetKey())
		if err != nil {
			return nil, err
		}
		if _, ok := expected[id]; !ok {
			return nil, fmt.Errorf("derived provider returned unowned key %q", id)
		}
		if _, duplicate := byKey[id]; duplicate {
			return nil, fmt.Errorf("derived provider returned duplicate key %q", id)
		}
		observation := normalizeObservation(item, deriver.Provider(), now)
		if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			observation.Dependencies = dependencyProofs(observation.GetKey(), base)
			observation.ValidUntil = earliestProofExpiry(observation.GetDependencies())
			observation.Evidence = capabilitycontract.DerivedEvidence(observation.GetDependencies()...)
			capabilitycontract.NormalizeObservation(observation)
		}
		byKey[id] = observation
	}
	if len(byKey) != len(expected) {
		return nil, fmt.Errorf("derived provider returned %d observations for %d owned keys", len(byKey), len(expected))
	}
	ordered := make([]*capabilityv1.CapabilityObservation, 0, len(byKey))
	for _, item := range byKey {
		ordered = append(ordered, item)
	}
	if err := validateBatch(deriver.Provider(), append(cloneObservationValues(base), ordered...), now); err != nil {
		return nil, err
	}
	return ordered, nil
}

func unknownDerived(expected map[string]*capabilityv1.CapabilityKey, provider capabilityv1.CapabilityProvider, now time.Time, reason string) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(expected))
	for _, key := range expected {
		out = append(out, unknownObservation(key, provider, now, reason))
	}
	return out
}

func dependencyProofs(key *capabilityv1.CapabilityKey, observations map[string]*capabilityv1.CapabilityObservation) []*capabilityv1.CapabilityObservationProof {
	keys, err := capabilitycontract.PlatformDependencyKeys(key.GetPlatform())
	if err != nil {
		return nil
	}
	proofs := make([]*capabilityv1.CapabilityObservationProof, 0, len(keys))
	for _, dependencyKey := range keys {
		id, _ := capabilitycontract.KeyID(dependencyKey)
		observation := observations[id]
		if observation == nil || observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
			return nil
		}
		proofs = append(proofs, capabilitycontract.NewObservationProof(observation))
	}
	return proofs
}

func earliestProofExpiry(proofs []*capabilityv1.CapabilityObservationProof) *timestamppb.Timestamp {
	var earliest time.Time
	for _, proof := range proofs {
		if proof.GetValidUntil() == nil {
			continue
		}
		value := proof.GetValidUntil().AsTime()
		if earliest.IsZero() || value.Before(earliest) {
			earliest = value
		}
	}
	if earliest.IsZero() {
		return nil
	}
	return timestamppb.New(earliest)
}

// AdmitDependencies validates the placement proof structurally, then binds
// the allocation to the latest valid proof for the exact same requirement set.
func (m *Manager) AdmitDependencies(dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityDependency, []*capabilityv1.CapabilityCondition, error) {
	if err := capabilitycontract.ValidateDependencySet(dependencies, now); err != nil {
		return nil, nil, fmt.Errorf("invalid placement capability proof: %w", err)
	}
	snapshot := m.Snapshot()
	if snapshot == nil || !m.Ready() {
		return nil, nil, fmt.Errorf("capability manager is warming")
	}
	keys := make([]*capabilityv1.CapabilityKey, 0, len(dependencies))
	selectedByKey := make(map[string]*capabilityv1.CapabilityObservationProof, len(dependencies))
	for _, dependency := range dependencies {
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		keys = append(keys, capabilitycontract.CloneKey(dependency.GetKey()))
		selectedByKey[id] = dependency.GetSelectedObservation()
	}
	admitted, err := capabilitycontract.ResolveDependencies(snapshot, keys, now)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve dependencies from snapshot %q: %w", snapshot.GetSnapshotID(), err)
	}
	conditions := make([]*capabilityv1.CapabilityCondition, 0, len(admitted))
	for _, dependency := range admitted {
		id, _ := capabilitycontract.KeyID(dependency.GetKey())
		proof := dependency.GetSelectedObservation()
		conditions = append(conditions, &capabilityv1.CapabilityCondition{
			Key: capabilitycontract.CloneKey(dependency.GetKey()), State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			Message:    evidenceReplacementMessage(selectedByKey[id], proof), ObservedAt: timestamppb.New(now.UTC()),
			Proof: proto.Clone(proof).(*capabilityv1.CapabilityObservationProof),
		})
	}
	return admitted, conditions, nil
}

func (m *Manager) VerifyDependencies(dependencies []*capabilityv1.CapabilityDependency, now time.Time) ([]*capabilityv1.CapabilityCondition, error) {
	_, conditions, err := m.AdmitDependencies(dependencies, now)
	return conditions, err
}

func evidenceReplacementMessage(selected, admitted *capabilityv1.CapabilityObservationProof) string {
	if selected != nil && admitted != nil && selected.GetObservationID() == admitted.GetObservationID() {
		return "placement observation remains valid"
	}
	selectedID := "<missing>"
	if selected != nil {
		selectedID = selected.GetObservationID()
	}
	admittedID := "<missing>"
	if admitted != nil {
		admittedID = admitted.GetObservationID()
	}
	return fmt.Sprintf("placement observation %s replaced by current observation %s", selectedID, admittedID)
}

func normalizeObservation(in *capabilityv1.CapabilityObservation, provider capabilityv1.CapabilityProvider, completedAt time.Time) *capabilityv1.CapabilityObservation {
	out := &capabilityv1.CapabilityObservation{}
	if in != nil {
		out = proto.Clone(in).(*capabilityv1.CapabilityObservation)
	}
	out.Provider = provider
	if out.ObservedAt == nil {
		out.ObservedAt = timestamppb.New(completedAt.UTC())
	}
	if out.State == capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED {
		out.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN
	}
	if out.ReasonCode == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_UNSPECIFIED {
		out.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR
	}
	if out.State == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE && out.ValidUntil == nil && out.GetKey().GetExtension() == nil {
		if definition, ok := capabilitycontract.PlatformDefinition(out.GetKey().GetPlatform()); ok && definition.Freshness.MaxValidity > 0 {
			out.ValidUntil = timestamppb.New(out.GetObservedAt().AsTime().Add(definition.Freshness.MaxValidity))
		}
	}
	capabilitycontract.NormalizeObservation(out)
	return out
}

func unknownObservation(key *capabilityv1.CapabilityKey, provider capabilityv1.CapabilityProvider, now time.Time, reason string) *capabilityv1.CapabilityObservation {
	observation := &capabilityv1.CapabilityObservation{
		Key: capabilitycontract.CloneKey(key), State: capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN, Provider: provider,
		ObservedAt: timestamppb.New(now.UTC()), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_ERROR,
		Reason: capabilitycontract.BoundedReason(reason),
	}
	capabilitycontract.NormalizeObservation(observation)
	return observation
}

func applyRecoveryPolicy(state *recoveryState, observation *capabilityv1.CapabilityObservation, completedAt time.Time) {
	if observation.GetState() != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		state.hadFailure = true
		state.firstSuccess = time.Time{}
		state.lastSuccessProof = ""
		state.successes = 0
		return
	}
	if !state.hadFailure {
		return
	}
	proof := recoverySampleProof(observation)
	if state.successes == 0 {
		state.firstSuccess = completedAt
		state.lastSuccessProof = proof
		state.successes = 1
	} else if completedAt.Sub(state.firstSuccess) >= recoveryConfirmationDelay && proof != "" && proof != state.lastSuccessProof {
		state.lastSuccessProof = proof
		state.successes++
	}
	if state.successes < 2 {
		observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED
		observation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_RECOVERY_PENDING
		observation.Reason = "capability recovery requires a second independent successful sample"
		return
	}
	*state = recoveryState{}
}

func recoverySampleProof(observation *capabilityv1.CapabilityObservation) string {
	if observation == nil {
		return ""
	}
	return observation.GetObservationID()
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
		_, changed := capabilitycontract.EvaluateObservationTransition(
			previous, old[id], previous.GetCollectedAt().AsTime(),
			current, observation, current.GetCollectedAt().AsTime(),
		)
		if !changed {
			continue
		}
		transitions = append(transitions, &Transition{Generation: current.GetSequence(), Key: capabilitycontract.CloneKey(observation.GetKey()), Previous: cloneObservation(old[id]), Current: cloneObservation(observation)})
	}
	return transitions
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
	callDeliverySafely("provider_probe_metrics", func() {
		metrics.RecordProviderProbe(provider, result, duration)
	})
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
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
		out[key] = cloneObservation(observation)
	}
	return out
}

func cloneObservationValues(in map[string]*capabilityv1.CapabilityObservation) []*capabilityv1.CapabilityObservation {
	out := make([]*capabilityv1.CapabilityObservation, 0, len(in))
	for _, observation := range in {
		out = append(out, cloneObservation(observation))
	}
	return out
}

func cloneSnapshot(in *capabilityv1.CapabilitySnapshot) *capabilityv1.CapabilitySnapshot {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*capabilityv1.CapabilitySnapshot)
}

func cloneKeys(in []*capabilityv1.CapabilityKey) []*capabilityv1.CapabilityKey {
	out := make([]*capabilityv1.CapabilityKey, 0, len(in))
	for _, key := range in {
		out = append(out, capabilitycontract.CloneKey(key))
	}
	return out
}
