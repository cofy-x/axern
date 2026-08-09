package service

import (
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type capabilityMetricsObserver struct{}

func (capabilityMetricsObserver) RecordProviderProbe(provider capabilityv1.CapabilityProvider, result string, duration time.Duration) {
	metrics.RecordCapabilityProbe(provider.String(), result, duration)
}

func (capabilityMetricsObserver) RecordSnapshot(snapshot *capabilityv1.CapabilitySnapshot) {
	if snapshot == nil {
		return
	}
	now := time.Now().UTC()
	metrics.RecordCapabilitySnapshotSequence(snapshot.GetSequence())
	for _, observation := range snapshot.GetObservations() {
		age := float64(0)
		if observation.GetObservedAt() != nil {
			age = max(0, now.Sub(observation.GetObservedAt().AsTime()).Seconds())
		}
		expiry := float64(-1)
		if observation.GetValidUntil() != nil {
			expiry = max(0, observation.GetValidUntil().AsTime().Sub(now).Seconds())
		}
		metrics.RecordCapabilityState(capabilityMetricLabel(observation.GetKey()), observation.GetProvider().String(), observation.GetState().String(), observation.GetReasonCode().String(), age, expiry)
	}
}

func (capabilityMetricsObserver) RecordTransitions(transitions []*nodecapability.Transition) {
	for _, transition := range transitions {
		current := transition.Current
		metrics.RecordCapabilityTransition(capabilityMetricLabel(transition.Key), current.GetProvider().String(), current.GetState().String(), current.GetReasonCode().String())
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
