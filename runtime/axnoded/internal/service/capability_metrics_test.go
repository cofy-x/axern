package service

import (
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAggregateCapabilityMetricsCountsCollapsedExtensions(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	snapshot := &capabilityv1.CapabilitySnapshot{Observations: []*capabilityv1.CapabilityObservation{
		{
			Key:        capabilitycontract.ExtensionKey("example.com/accelerator", "a"),
			Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
			State:      capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			ObservedAt: timestamppb.New(now.Add(-time.Second)),
		},
		{
			Key:        capabilitycontract.ExtensionKey("example.net/feature", "b"),
			Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
			State:      capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			ObservedAt: timestamppb.New(now.Add(-3 * time.Second)),
		},
	}}
	states, observations := aggregateCapabilityMetrics(snapshot, now)
	series := capabilityMetricSeries{
		capability: "extension",
		provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG.String(),
		state:      capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE.String(),
		reason:     capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE.String(),
	}
	if states[series] != 2 {
		t.Fatalf("collapsed extension state count = %v, want 2", states[series])
	}
	identity := capabilityMetricIdentity{capability: series.capability, provider: series.provider}
	if observations[identity].maxAge != 3 || observations[identity].hasExpiry {
		t.Fatalf("collapsed extension observation = %#v", observations[identity])
	}
}
