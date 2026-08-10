package service

import (
	"sync"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type capabilityMetricSeries struct {
	capability string
	provider   string
	state      string
	reason     string
}

type capabilityMetricIdentity struct {
	capability string
	provider   string
}

type capabilityMetricAggregate struct {
	maxAge    float64
	minExpiry float64
	hasExpiry bool
}

type capabilityMetricsObserver struct {
	mu       sync.Mutex
	previous map[capabilityMetricSeries]struct{}
}

func (*capabilityMetricsObserver) RecordProviderProbe(provider capabilityv1.CapabilityProvider, result string, duration time.Duration) {
	metrics.RecordCapabilityProbe(provider.String(), result, duration)
}

func (o *capabilityMetricsObserver) RecordSnapshot(snapshot *capabilityv1.CapabilitySnapshot) {
	if snapshot == nil {
		return
	}
	now := time.Now().UTC()
	metrics.RecordCapabilitySnapshotSequence(snapshot.GetSequence())
	states, observations := aggregateCapabilityMetrics(snapshot, now)
	o.mu.Lock()
	defer o.mu.Unlock()
	for previous := range o.previous {
		if _, exists := states[previous]; exists {
			continue
		}
		metrics.RecordCapabilityState(previous.capability, previous.provider, previous.state, previous.reason, 0)
	}
	next := make(map[capabilityMetricSeries]struct{}, len(states))
	for series, count := range states {
		metrics.RecordCapabilityState(series.capability, series.provider, series.state, series.reason, count)
		next[series] = struct{}{}
	}
	for identity, aggregate := range observations {
		expiry := float64(-1)
		if aggregate.hasExpiry {
			expiry = aggregate.minExpiry
		}
		metrics.RecordCapabilityObservation(identity.capability, identity.provider, aggregate.maxAge, expiry)
	}
	o.previous = next
}

func aggregateCapabilityMetrics(snapshot *capabilityv1.CapabilitySnapshot, now time.Time) (map[capabilityMetricSeries]float64, map[capabilityMetricIdentity]capabilityMetricAggregate) {
	states := make(map[capabilityMetricSeries]float64)
	observations := make(map[capabilityMetricIdentity]capabilityMetricAggregate)
	for _, observation := range snapshot.GetObservations() {
		if observation == nil {
			continue
		}
		age := float64(0)
		if observation.GetObservedAt() != nil {
			age = max(0, now.Sub(observation.GetObservedAt().AsTime()).Seconds())
		}
		series := capabilityMetricSeries{
			capability: capabilityMetricLabel(observation.GetKey()), provider: observation.GetProvider().String(),
			state: observation.GetState().String(), reason: observation.GetReasonCode().String(),
		}
		states[series]++
		identity := capabilityMetricIdentity{capability: series.capability, provider: series.provider}
		aggregate := observations[identity]
		aggregate.maxAge = max(aggregate.maxAge, age)
		if observation.GetValidUntil() != nil {
			expiry := max(0, observation.GetValidUntil().AsTime().Sub(now).Seconds())
			if !aggregate.hasExpiry || expiry < aggregate.minExpiry {
				aggregate.minExpiry = expiry
			}
			aggregate.hasExpiry = true
		}
		observations[identity] = aggregate
	}
	return states, observations
}

func (*capabilityMetricsObserver) RecordTransitions(transitions []*nodecapability.Transition) {
	for _, transition := range transitions {
		current := transition.Current
		capability := capabilityMetricLabel(transition.Key)
		provider := current.GetProvider().String()
		metrics.RecordCapabilityTransition(capability, provider, current.GetState().String(), current.GetReasonCode().String())
		if current.GetReasonCode() == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_RECOVERY_PENDING {
			metrics.RecordCapabilityRecoveryDebounce(capability, provider)
		}
	}
}

func capabilityMetricLabel(key *capabilityv1.CapabilityKey) string {
	if key.GetExtension() != nil {
		return "extension"
	}
	if _, ok := capabilitycontract.PlatformDefinition(key.GetPlatform()); !ok {
		return "unknown"
	}
	return key.GetPlatform().String()
}
