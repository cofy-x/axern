package pgnodes

import (
	"testing"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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
