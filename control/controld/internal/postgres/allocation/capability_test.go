package pgallocation

import (
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateClaimedCapabilityWorkRequiresExactDurableDependencies(t *testing.T) {
	runcKey := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)
	runcKeyID, err := capabilitycontract.KeyID(runcKey)
	if err != nil {
		t.Fatal(err)
	}
	dependency := &capabilityv1.CapabilityDependency{Key: runcKey}
	valid := allocationkernel.CapabilityReconcileItem{
		AllocationID:       "allocation-a",
		Dependencies:       []*capabilityv1.CapabilityDependency{dependency},
		PendingGenerations: map[string]int64{runcKeyID: 7},
	}
	if err := validateClaimedCapabilityWork(valid); err != nil {
		t.Fatalf("valid claimed work rejected: %v", err)
	}
	missing := valid
	missing.Dependencies = nil
	if err := validateClaimedCapabilityWork(missing); err == nil {
		t.Fatal("pending generation without a durable dependency was accepted")
	}
	duplicate := valid
	duplicate.Dependencies = []*capabilityv1.CapabilityDependency{dependency, dependency}
	if err := validateClaimedCapabilityWork(duplicate); err == nil {
		t.Fatal("duplicate durable dependencies were accepted")
	}
}

func TestCapabilityConditionSetDigestCanonicalizesConditionOrder(t *testing.T) {
	now := time.Now().UTC()
	port := &capabilityv1.CapabilityCondition{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)}
	network := &capabilityv1.CapabilityCondition{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE)}
	left, err := canonicalCapabilityConditionSet(&capabilityv1.CapabilityConditionSet{Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{port, network}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalCapabilityConditionSet(&capabilityv1.CapabilityConditionSet{Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{network, port}})
	if err != nil {
		t.Fatal(err)
	}
	leftDigest, err := capabilityConditionSetDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := capabilityConditionSetDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("condition digest depends on repeated-field order: %q != %q", leftDigest, rightDigest)
	}
}

func TestCapabilityDependencySetDigestCanonicalizesDependencyOrder(t *testing.T) {
	runc := &capabilityv1.CapabilityDependency{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)}
	runsc := &capabilityv1.CapabilityDependency{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT)}
	left, err := capabilityDependencySetDigest([]*capabilityv1.CapabilityDependency{runc, runsc})
	if err != nil {
		t.Fatal(err)
	}
	right, err := capabilityDependencySetDigest([]*capabilityv1.CapabilityDependency{runsc, runc})
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("dependency digest depends on repeated-field order: %q != %q", left, right)
	}
}

func TestCreateAdmissionConditionMustBindAdmittedObservation(t *testing.T) {
	now := time.Now().UTC()
	key := capabilitycontract.ExtensionKey("example.com/accelerator", "v1")
	observation := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
		ObservedAt: timestamppb.New(now), Evidence: capabilitycontract.ConfigEvidence("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	dependency := &capabilityv1.CapabilityDependency{Key: key, SelectedObservation: capabilitycontract.NewObservationProof(observation)}
	condition := &capabilityv1.CapabilityCondition{
		Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		ObservedAt: timestamppb.New(now), Proof: proto.Clone(dependency.GetSelectedObservation()).(*capabilityv1.CapabilityObservationProof),
	}
	set := &capabilityv1.CapabilityConditionSet{Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{condition}}
	if err := validateCreateAdmissionConditions([]*capabilityv1.CapabilityDependency{dependency}, set); err != nil {
		t.Fatalf("matching create proof rejected: %v", err)
	}
	replacement := proto.Clone(observation).(*capabilityv1.CapabilityObservation)
	replacement.ObservedAt = timestamppb.New(now.Add(time.Millisecond))
	capabilitycontract.NormalizeObservation(replacement)
	set.Conditions[0].Proof = capabilitycontract.NewObservationProof(replacement)
	if err := validateCreateAdmissionConditions([]*capabilityv1.CapabilityDependency{dependency}, set); err == nil {
		t.Fatal("create condition with a different valid observation proof was accepted")
	}
}

func TestSameDependencyKeysRequiresAnExactSet(t *testing.T) {
	runc := &capabilityv1.CapabilityDependency{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)}
	runsc := &capabilityv1.CapabilityDependency{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT)}
	if !sameDependencyKeys([]*capabilityv1.CapabilityDependency{runc, runsc}, []*capabilityv1.CapabilityDependency{runsc, runc}) {
		t.Fatal("same dependency keys should ignore ordering")
	}
	if sameDependencyKeys([]*capabilityv1.CapabilityDependency{runc}, []*capabilityv1.CapabilityDependency{runsc}) {
		t.Fatal("different dependency keys were accepted")
	}
	if sameDependencyKeys([]*capabilityv1.CapabilityDependency{runc, runc}, []*capabilityv1.CapabilityDependency{runc, runc}) {
		t.Fatal("duplicate dependency keys were accepted")
	}
}

func TestShouldReplaceCapabilityConditionsFencesAttemptAndRevision(t *testing.T) {
	tests := []struct {
		name                                string
		storedAttempt, storedRevision       int64
		allocationAttempt, incomingRevision int64
		wantReplace, wantError              bool
	}{
		{name: "new attempt restarts revision", storedAttempt: 1, storedRevision: 9, allocationAttempt: 2, incomingRevision: 1, wantReplace: true},
		{name: "newer revision in same attempt", storedAttempt: 2, storedRevision: 1, allocationAttempt: 2, incomingRevision: 2, wantReplace: true},
		{name: "duplicate revision", storedAttempt: 2, storedRevision: 2, allocationAttempt: 2, incomingRevision: 2},
		{name: "stale revision", storedAttempt: 2, storedRevision: 3, allocationAttempt: 2, incomingRevision: 2},
		{name: "stored future attempt is corruption", storedAttempt: 3, storedRevision: 1, allocationAttempt: 2, incomingRevision: 2, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			replace, err := shouldReplaceCapabilityConditions(test.storedAttempt, test.storedRevision, test.allocationAttempt, test.incomingRevision)
			if (err != nil) != test.wantError || replace != test.wantReplace {
				t.Fatalf("replace=%v error=%v, want replace=%v error=%v", replace, err, test.wantReplace, test.wantError)
			}
		})
	}
}
