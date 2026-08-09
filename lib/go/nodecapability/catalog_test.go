package nodecapability

import (
	"testing"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValidateExtensionRejectsReservedAndUnqualifiedNames(t *testing.T) {
	for _, name := range []string{"gpu", "axern.io/gpu", "axern.dev/feature", "bad_domain/gpu"} {
		if err := ValidateExtension(&capabilityv1.ExtensionCapability{Name: name}); err == nil {
			t.Fatalf("ValidateExtension(%q) succeeded", name)
		}
	}
	if err := ValidateExtension(&capabilityv1.ExtensionCapability{Name: "example.com/gpu", Value: "a100"}); err != nil {
		t.Fatalf("ValidateExtension() error = %v", err)
	}
}

func TestExtensionIdentityCanonicalizesOnlyDNSDomain(t *testing.T) {
	key := ExtensionKey(" Example.COM/Accelerator ", " exact value ")
	if key.GetExtension().GetName() != "example.com/Accelerator" || key.GetExtension().GetValue() != " exact value " {
		t.Fatalf("normalized extension = %#v", key.GetExtension())
	}
	left, err := KeyID(key)
	if err != nil {
		t.Fatal(err)
	}
	right, err := KeyID(ExtensionKey("example.com/Accelerator", " exact value "))
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("canonical IDs differ: %q != %q", left, right)
	}
	if sameValueTrimmed, _ := KeyID(ExtensionKey("example.com/Accelerator", "exact value")); sameValueTrimmed == left {
		t.Fatal("exact extension value was trimmed while building its identity")
	}
}

func TestAvailableObservationFailsClosedOnExpiryAndDependencyIdentity(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	baseKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	derivedKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	base := observation(baseKey, "base-v1", now)
	selfTest := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST), "self-test-v1", now)
	derived := observation(derivedKey, "derived-v1", now)
	derived.ValidUntil = timestamppb.New(now.Add(10 * time.Second))
	derived.Dependencies = []*capabilityv1.CapabilityEvidenceReference{
		{Key: baseKey, EvidenceID: "base-v1", Evidence: base.Evidence},
		{Key: selfTest.GetKey(), EvidenceID: "self-test-v1", Evidence: selfTest.Evidence},
	}
	snapshot := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 1, SnapshotID: "snapshot", CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{base, selfTest, derived}}
	if _, ok := AvailableObservation(snapshot, derivedKey, now); !ok {
		t.Fatal("derived observation should be available")
	}
	base.Evidence.EvidenceID = "base-v2"
	if _, ok := AvailableObservation(snapshot, derivedKey, now); ok {
		t.Fatal("dependency identity mismatch must fail closed")
	}
	base.Evidence.EvidenceID = "base-v1"
	if _, ok := AvailableObservation(snapshot, derivedKey, now.Add(11*time.Second)); ok {
		t.Fatal("expired observation must fail closed")
	}
}

func TestResolveDependenciesUsesCatalogLossPolicyAndEvidence(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)
	available := observation(key, "runtime-proof", now)
	cgroup := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER), "cgroup-proof", now)
	selfTest := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST), "self-test-proof", now)
	available.Dependencies = []*capabilityv1.CapabilityEvidenceReference{
		{Key: cgroup.GetKey(), EvidenceID: cgroup.GetEvidence().GetEvidenceID(), Evidence: cgroup.GetEvidence()},
		{Key: selfTest.GetKey(), EvidenceID: selfTest.GetEvidence().GetEvidenceID(), Evidence: selfTest.GetEvidence()},
	}
	snapshot := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 1, SnapshotID: "snapshot", CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{available, cgroup, selfTest}}
	dependencies, err := ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key, key}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 1 || dependencies[0].GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP || dependencies[0].GetSelectedEvidence().GetEvidenceID() != "runtime-proof" {
		t.Fatalf("dependencies = %#v", dependencies)
	}
}

func TestValidateSnapshotRejectsWrongCatalogOwnerAndScope(t *testing.T) {
	now := time.Now().UTC()
	item := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER), "proof", now)
	item.Provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG
	snapshot := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 1, SnapshotID: "snapshot", CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{item}}
	if err := ValidateSnapshot(snapshot, now); err == nil {
		t.Fatal("ValidateSnapshot() accepted a capability published by the wrong provider")
	}
}

func TestValidateSnapshotRejectsContradictoryHealthMetadata(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	tests := []struct {
		name   string
		mutate func(*capabilityv1.CapabilityObservation)
	}{
		{name: "available with failure reason", mutate: func(item *capabilityv1.CapabilityObservation) {
			item.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
		}},
		{name: "unavailable with available reason", mutate: func(item *capabilityv1.CapabilityObservation) {
			item.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
		}},
		{name: "expiry before observation", mutate: func(item *capabilityv1.CapabilityObservation) {
			item.ValidUntil = timestamppb.New(now.Add(-time.Second))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := observation(key, "proof", now)
			test.mutate(item)
			snapshot := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 1, SnapshotID: "snapshot", CollectedAt: timestamppb.New(now), Observations: []*capabilityv1.CapabilityObservation{item}}
			if err := ValidateSnapshot(snapshot, now); err == nil {
				t.Fatal("ValidateSnapshot() accepted contradictory health metadata")
			}
		})
	}
}

func observation(key *capabilityv1.CapabilityKey, evidenceID string, now time.Time) *capabilityv1.CapabilityObservation {
	provider, scope, err := ObservationOwner(key)
	if err != nil {
		panic(err)
	}
	evidence := &capabilityv1.CapabilityEvidence{EvidenceID: evidenceID}
	switch scope {
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC:
		evidence.ConfigDigest = "config"
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT:
		evidence.BootID = "boot"
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT:
		evidence.BootID, evidence.MountIdentity = "boot", "mount"
	case capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME:
		evidence.BootID, evidence.RuntimeName, evidence.RuntimeBinaryDigest, evidence.ConfigDigest = "boot", "runc", "binary", "config"
	}
	result := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider: provider, ValidityScope: scope, ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		ObservedAt: timestamppb.New(now), Evidence: evidence,
	}
	if scope == capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE {
		result.ValidUntil = timestamppb.New(now.Add(time.Minute))
	}
	return result
}
