package pgnodes

import (
	"testing"
	"time"

	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateSummaryPublicationFencesFreshnessClock(t *testing.T) {
	now := time.Now().UTC()
	snapshot := &capabilityv1.CapabilitySnapshot{CollectedAt: timestamppb.New(now)}
	summary := &nodev1.NodeSummary{NodeInstanceID: "instance-1", Sequence: 1, CollectedAt: timestamppb.New(now), CapabilitySnapshot: snapshot}
	if err := validateSummaryPublication(summary, now); err != nil {
		t.Fatal(err)
	}
	future := proto.Clone(summary).(*nodev1.NodeSummary)
	future.CollectedAt = timestamppb.New(now.Add(2 * time.Minute))
	if err := validateSummaryPublication(future, now); err == nil {
		t.Fatal("future node summary could extend placement freshness")
	}
	misordered := proto.Clone(summary).(*nodev1.NodeSummary)
	misordered.CollectedAt = timestamppb.New(now.Add(-time.Second))
	if err := validateSummaryPublication(misordered, now); err == nil {
		t.Fatal("node summary accepted a capability snapshot published after it")
	}
}

func TestValidateNodeObservationAdvanceAllowsOnlyExactReplay(t *testing.T) {
	now := timestamppb.New(time.Now().UTC())
	previous := &nodev1.NodeSummary{
		NodeInstanceID: "instance-1", Sequence: 7, CollectedAt: now,
		CapabilitySnapshot: &capabilityv1.CapabilitySnapshot{CollectedAt: now},
		MemoryBudget:       &nodev1.NodeMemoryBudget{SampledAt: now},
	}
	replay := proto.Clone(previous).(*nodev1.NodeSummary)
	idempotent, err := validateNodeObservationAdvance(previous, replay)
	if err != nil || !idempotent {
		t.Fatalf("exact replay = (%t, %v), want idempotent", idempotent, err)
	}
	changed := proto.Clone(previous).(*nodev1.NodeSummary)
	changed.NodeState = nodev1.NodeState_NODE_STATE_DRAINING
	if _, err := validateNodeObservationAdvance(previous, changed); err == nil {
		t.Fatal("same sequence with different whole-node observation was accepted")
	}
	stale := proto.Clone(previous).(*nodev1.NodeSummary)
	stale.Sequence--
	if _, err := validateNodeObservationAdvance(previous, stale); err == nil {
		t.Fatal("decreasing sequence was accepted")
	}
	next := proto.Clone(previous).(*nodev1.NodeSummary)
	next.Sequence++
	if idempotent, err := validateNodeObservationAdvance(previous, next); err != nil || idempotent {
		t.Fatalf("next sequence = (%t, %v), want accepted advance", idempotent, err)
	}
	restarted := proto.Clone(previous).(*nodev1.NodeSummary)
	restarted.NodeInstanceID = "instance-2"
	restarted.Sequence = 1
	if idempotent, err := validateNodeObservationAdvance(previous, restarted); err != nil || idempotent {
		t.Fatalf("new instance = (%t, %v), want accepted reset", idempotent, err)
	}
	regressed := proto.Clone(next).(*nodev1.NodeSummary)
	regressed.Sequence++
	regressed.CollectedAt = timestamppb.New(previous.GetCollectedAt().AsTime().Add(-time.Second))
	if _, err := validateNodeObservationAdvance(next, regressed); err == nil {
		t.Fatal("snapshot with regressed collected_at was accepted")
	}
	capabilityRegressed := proto.Clone(next).(*nodev1.NodeSummary)
	capabilityRegressed.Sequence++
	capabilityRegressed.CollectedAt = timestamppb.New(next.GetCollectedAt().AsTime().Add(time.Second))
	capabilityRegressed.CapabilitySnapshot.CollectedAt = timestamppb.New(previous.GetCapabilitySnapshot().GetCollectedAt().AsTime().Add(-time.Second))
	if _, err := validateNodeObservationAdvance(next, capabilityRegressed); err == nil {
		t.Fatal("snapshot with regressed capability sample was accepted")
	}
	memoryRegressed := proto.Clone(next).(*nodev1.NodeSummary)
	memoryRegressed.Sequence++
	memoryRegressed.CollectedAt = timestamppb.New(next.GetCollectedAt().AsTime().Add(time.Second))
	memoryRegressed.MemoryBudget.SampledAt = timestamppb.New(previous.GetMemoryBudget().GetSampledAt().AsTime().Add(-time.Second))
	if _, err := validateNodeObservationAdvance(next, memoryRegressed); err == nil {
		t.Fatal("snapshot with regressed memory boundary sample was accepted")
	}
}

func TestValidateSummaryPublicationRejectsInvalidObservationIdentity(t *testing.T) {
	now := time.Now().UTC()
	base := &nodev1.NodeSummary{
		NodeInstanceID: "instance-1", Sequence: 1, CollectedAt: timestamppb.New(now),
		CapabilitySnapshot: &capabilityv1.CapabilitySnapshot{CollectedAt: timestamppb.New(now)},
	}
	for _, instanceID := range []string{"", " instance-1", string([]byte{0xff}), string(make([]byte, maxNodeObservationInstanceIDBytes+1))} {
		summary := proto.Clone(base).(*nodev1.NodeSummary)
		summary.NodeInstanceID = instanceID
		if err := validateSummaryPublication(summary, now); err == nil {
			t.Fatalf("node_instance_id %q was accepted", instanceID)
		}
	}
}

func TestValidateNodeObservationAdvanceRejectsOwnershipChangeWithinNodeInstance(t *testing.T) {
	now := timestamppb.New(time.Now().UTC())
	firstKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	secondKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	previous := &nodev1.NodeSummary{
		NodeInstanceID: "instance-1", Sequence: 1, CollectedAt: now,
		CapabilitySnapshot: &capabilityv1.CapabilitySnapshot{CollectedAt: now, Observations: []*capabilityv1.CapabilityObservation{{Key: firstKey}, {Key: secondKey}}},
	}
	next := proto.Clone(previous).(*nodev1.NodeSummary)
	next.Sequence = 2
	next.CapabilitySnapshot.Observations = next.CapabilitySnapshot.Observations[:1]
	if _, err := validateNodeObservationAdvance(previous, next); err == nil {
		t.Fatal("same node instance removed an owned capability observation")
	}

	next.NodeInstanceID = "instance-2"
	next.Sequence = 1
	if idempotent, err := validateNodeObservationAdvance(previous, next); err != nil || idempotent {
		t.Fatalf("new node instance ownership change = (%t, %v), want accepted reset", idempotent, err)
	}
}

func TestCapabilityTransitionPreservesPreviouslyPublishedAvailableState(t *testing.T) {
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	available := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt: timestamppb.New(observedAt), ValidUntil: timestamppb.New(observedAt.Add(capabilitycontract.HealthObservationValidity)),
		Evidence:   nil,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(available)
	previous := &capabilityv1.CapabilitySnapshot{
		CollectedAt: timestamppb.New(observedAt), Observations: []*capabilityv1.CapabilityObservation{available},
	}
	unknown := &capabilityv1.CapabilityObservation{
		Key: capabilitycontract.CloneKey(key), State: capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt: timestamppb.New(observedAt.Add(16 * time.Second)),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED,
		Reason:     "health observation expired",
	}
	capabilitycontract.NormalizeObservation(unknown)
	next := &capabilityv1.CapabilitySnapshot{
		CollectedAt: timestamppb.New(observedAt.Add(16 * time.Second)), Observations: []*capabilityv1.CapabilityObservation{unknown},
	}

	transitions, err := capabilityTransitions(previous, next, observedAt.Add(16*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(transitions))
	}
	if transitions[0].oldState != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || transitions[0].newState != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN {
		t.Fatalf("transition states = %s -> %s, want AVAILABLE -> UNKNOWN", transitions[0].oldState, transitions[0].newState)
	}
}

func TestCapabilityTransitionRecordsEffectiveExpiryWithoutRawStateChange(t *testing.T) {
	observedAt := time.Now().UTC().Truncate(time.Microsecond)
	key := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	available := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		ObservedAt: timestamppb.New(observedAt), ValidUntil: timestamppb.New(observedAt.Add(capabilitycontract.HealthObservationValidity)),
		Evidence:   nil,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(available)
	previous := &capabilityv1.CapabilitySnapshot{
		CollectedAt: timestamppb.New(observedAt), Observations: []*capabilityv1.CapabilityObservation{available},
	}
	next := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	next.CollectedAt = timestamppb.New(observedAt.Add(capabilitycontract.HealthObservationValidity + time.Second))

	transitions, err := capabilityTransitions(previous, next, next.GetCollectedAt().AsTime())
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || transitions[0].oldState != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || transitions[0].newState != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN {
		t.Fatalf("effective expiry transitions = %#v, want AVAILABLE -> UNKNOWN", transitions)
	}
	if transitions[0].reasonCode != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED {
		t.Fatalf("effective expiry reason = %s, want EXPIRED", transitions[0].reasonCode)
	}
}
