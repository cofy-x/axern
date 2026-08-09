package pgnodes

import (
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateSummaryPublicationFencesFreshnessClock(t *testing.T) {
	now := time.Now().UTC()
	snapshot := &capabilityv1.CapabilitySnapshot{CollectedAt: timestamppb.New(now)}
	summary := &nodev1.NodeSummary{CollectedAt: timestamppb.New(now), CapabilitySnapshot: snapshot}
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

func TestValidateSnapshotAdvanceAllowsOnlyExactReplay(t *testing.T) {
	now := timestamppb.New(time.Now().UTC())
	previous := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance-1", Sequence: 7, SnapshotID: "snapshot-7", CollectedAt: now}
	replay := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	idempotent, err := validateSnapshotAdvance(previous, replay)
	if err != nil || !idempotent {
		t.Fatalf("exact replay = (%t, %v), want idempotent", idempotent, err)
	}
	changed := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	changed.SnapshotID = "different"
	if _, err := validateSnapshotAdvance(previous, changed); err == nil {
		t.Fatal("same sequence with different snapshot was accepted")
	}
	stale := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	stale.Sequence--
	if _, err := validateSnapshotAdvance(previous, stale); err == nil {
		t.Fatal("decreasing sequence was accepted")
	}
	next := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	next.Sequence++
	next.SnapshotID = "snapshot-8"
	if idempotent, err := validateSnapshotAdvance(previous, next); err != nil || idempotent {
		t.Fatalf("next sequence = (%t, %v), want accepted advance", idempotent, err)
	}
	restarted := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	restarted.NodeInstanceID = "instance-2"
	restarted.Sequence = 1
	restarted.SnapshotID = "instance-2-snapshot-1"
	if idempotent, err := validateSnapshotAdvance(previous, restarted); err != nil || idempotent {
		t.Fatalf("new instance = (%t, %v), want accepted reset", idempotent, err)
	}
	regressed := proto.Clone(next).(*capabilityv1.CapabilitySnapshot)
	regressed.Sequence++
	regressed.SnapshotID = "snapshot-9"
	regressed.CollectedAt = timestamppb.New(previous.GetCollectedAt().AsTime().Add(-time.Second))
	if _, err := validateSnapshotAdvance(next, regressed); err == nil {
		t.Fatal("snapshot with regressed collected_at was accepted")
	}
}

func TestValidateSnapshotAdvanceRejectsOwnershipChangeWithinNodeInstance(t *testing.T) {
	now := timestamppb.New(time.Now().UTC())
	firstKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	secondKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)
	previous := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-1", Sequence: 1, SnapshotID: "snapshot-1", CollectedAt: now,
		Observations: []*capabilityv1.CapabilityObservation{{Key: firstKey}, {Key: secondKey}},
	}
	next := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	next.Sequence = 2
	next.SnapshotID = "snapshot-2"
	next.Observations = next.Observations[:1]
	if _, err := validateSnapshotAdvance(previous, next); err == nil {
		t.Fatal("same node instance removed an owned capability observation")
	}

	next.NodeInstanceID = "instance-2"
	next.Sequence = 1
	if idempotent, err := validateSnapshotAdvance(previous, next); err != nil || idempotent {
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
		Evidence:   capabilitycontract.ConfigEvidence("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(available)
	previous := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-1", Sequence: 1, SnapshotID: "snapshot-1",
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
		NodeInstanceID: "instance-1", Sequence: 2, SnapshotID: "snapshot-2",
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
		Evidence:   capabilitycontract.ConfigEvidence("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(available)
	previous := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-1", Sequence: 1, SnapshotID: "snapshot-1",
		CollectedAt: timestamppb.New(observedAt), Observations: []*capabilityv1.CapabilityObservation{available},
	}
	next := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	next.Sequence = 2
	next.SnapshotID = "snapshot-2"
	next.CollectedAt = timestamppb.New(observedAt.Add(capabilitycontract.HealthObservationValidity + time.Second))

	transitions, err := capabilityTransitions(previous, next, next.GetCollectedAt().AsTime())
	if err != nil {
		t.Fatal(err)
	}
	if len(transitions) != 1 || transitions[0].oldState != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE || transitions[0].newState != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN {
		t.Fatalf("effective expiry transitions = %#v, want AVAILABLE -> UNKNOWN", transitions)
	}
	if transitions[0].newReasonCode != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED {
		t.Fatalf("effective expiry reason = %s, want EXPIRED", transitions[0].newReasonCode)
	}
}
