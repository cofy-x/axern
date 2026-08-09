package nodecapability

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const testBootID = "11111111-2222-3333-4444-555555555555"

func TestCatalogIsCompleteAndAcyclic(t *testing.T) {
	if err := validateCatalog(definitions); err != nil {
		t.Fatal(err)
	}
	if digest := CatalogDigest(); !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		t.Fatalf("CatalogDigest() = %q", digest)
	}
}

func TestValidateExtensionRejectsReservedUnqualifiedAndOversizedValues(t *testing.T) {
	for _, capability := range []*capabilityv1.ExtensionCapability{
		{Name: "gpu"},
		{Name: "axern.io/gpu"},
		{Name: "internal.axern.io/gpu"},
		{Name: "axern.dev/feature"},
		{Name: "bad_domain/gpu"},
		{Name: " example.com/gpu"},
		{Name: "example.com/gpu "},
		{Name: "example.com/gpu", Value: string([]byte{0xff})},
		{Name: "example.com/gpu", Value: strings.Repeat("x", MaxExtensionValueBytes+1)},
	} {
		if err := ValidateExtension(capability); err == nil {
			t.Fatalf("ValidateExtension(%q) succeeded", capability.GetName())
		}
	}
	if err := ValidateExtension(&capabilityv1.ExtensionCapability{Name: "example.com/gpu", Value: "a100"}); err != nil {
		t.Fatalf("ValidateExtension() error = %v", err)
	}
}

func TestCatalogRejectsInvalidDerivedAndLossPolicyBoundaries(t *testing.T) {
	cloneDefinitions := func() map[capabilityv1.PlatformCapability]Definition {
		result := make(map[capabilityv1.PlatformCapability]Definition, len(definitions))
		for key, definition := range definitions {
			definition.Dependencies = append([]capabilityv1.PlatformCapability(nil), definition.Dependencies...)
			result[key] = definition
		}
		return result
	}
	t.Run("derived consumes derived", func(t *testing.T) {
		catalog := cloneDefinitions()
		definition := catalog[capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT]
		definition.Dependencies = []capabilityv1.PlatformCapability{capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT}
		catalog[definition.Key] = definition
		if err := validateCatalog(catalog); err == nil {
			t.Fatal("catalog accepted a derived capability dependency")
		}
	})
	t.Run("fail stop without verifier", func(t *testing.T) {
		catalog := cloneDefinitions()
		definition := catalog[capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS]
		definition.LossPolicy = capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP
		catalog[definition.Key] = definition
		if err := validateCatalog(catalog); err == nil {
			t.Fatal("catalog accepted fail-stop policy without an allocation verifier")
		}
	})
}

func TestExtensionRequirementsRejectMultipleValuesForOneName(t *testing.T) {
	requirements := []*capabilityv1.ExtensionCapabilityRequirement{
		{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "a100"}},
		{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "h100"}},
	}
	if err := ValidateExtensionRequirements(requirements); err == nil {
		t.Fatal("multiple exact values for one extension capability name were accepted")
	}
}

func TestBoundedReasonProducesValidUTF8WithinByteLimit(t *testing.T) {
	reason := string([]byte{0xff}) + strings.Repeat("界", MaxReasonBytes)
	bounded := BoundedReason(reason)
	if !utf8.ValidString(bounded) {
		t.Fatal("BoundedReason returned invalid UTF-8")
	}
	if len(bounded) > MaxReasonBytes {
		t.Fatalf("BoundedReason length = %d, want <= %d", len(bounded), MaxReasonBytes)
	}
}

func TestExtensionIdentityCanonicalizesOnlyDNSDomain(t *testing.T) {
	key := ExtensionKey("Example.COM/Accelerator", " exact value ")
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

func TestExtensionKeyIDIsPostgresSafeAndCollisionFree(t *testing.T) {
	left, err := KeyID(ExtensionKey("example.com/feature", "a/b\nvalue"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := KeyID(ExtensionKey("example.com/feature", "a/b\nvalue-2"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(left, '\x00') || left == right {
		t.Fatalf("extension key IDs are not safe and distinct: left=%q right=%q", left, right)
	}
}

func TestIdentityAndFreshnessAreOrthogonal(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	item := observation(key, now)
	if EvidenceIdentityKind(item.GetEvidence()) != IdentityMount || item.GetValidUntil() == nil {
		t.Fatalf("mount observation = %#v", item)
	}
	item.ValidUntil = nil
	NormalizeObservation(item)
	snapshot := testSnapshot(now, item)
	if err := ValidateSnapshot(snapshot, now); err == nil {
		t.Fatal("mount identity without its independent TTL was accepted")
	}
}

func TestAvailableObservationFailsClosedOnExpiryAndDependencyIdentity(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	baseKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	derivedKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	base := observation(baseKey, now)
	selfTest := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST), now)
	derived := derivedObservation(derivedKey, now, base, selfTest)
	snapshot := testSnapshot(now, base, selfTest, derived)
	if _, ok := AvailableObservation(snapshot, derivedKey, now); !ok {
		t.Fatal("derived observation should be available")
	}

	base.Evidence = MountEvidence(testBootID, "mount-v2")
	NormalizeObservation(base)
	snapshot = testSnapshot(now, base, selfTest, derived)
	if _, ok := AvailableObservation(snapshot, derivedKey, now); ok {
		t.Fatal("dependency identity mismatch must fail closed")
	}

	base = observation(baseKey, now)
	derived = derivedObservation(derivedKey, now, base, selfTest)
	snapshot = testSnapshot(now.Add(16*time.Second), base, selfTest, derived)
	if _, ok := AvailableObservation(snapshot, derivedKey, now.Add(16*time.Second)); ok {
		t.Fatal("expired observation in a fresh snapshot must fail closed")
	}
}

func TestDerivedEvidenceIdentityTracksDependencySubjectsNotTTLRefresh(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	base := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER), now)
	selfTest := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST), now)
	first := DerivedEvidence(NewObservationProof(base), NewObservationProof(selfTest))

	refreshed := observation(base.GetKey(), now.Add(5*time.Second))
	second := DerivedEvidence(NewObservationProof(refreshed), NewObservationProof(selfTest))
	if first.GetEvidenceID() != second.GetEvidenceID() {
		t.Fatal("ordinary dependency TTL refresh changed derived evidence identity")
	}

	refreshed.Evidence = MountEvidence(testBootID, "mount-v2")
	NormalizeObservation(refreshed)
	third := DerivedEvidence(NewObservationProof(refreshed), NewObservationProof(selfTest))
	if first.GetEvidenceID() == third.GetEvidenceID() {
		t.Fatal("dependency subject identity change did not change derived evidence identity")
	}
}

func TestResolveDependenciesPersistsSnapshotAndCompleteProof(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT)
	cgroup := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER), now)
	selfTest := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST), now)
	available := derivedObservation(key, now, cgroup, selfTest)
	snapshot := testSnapshot(now, available, cgroup, selfTest)
	dependencies, err := ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(dependencies) != 1 || dependencies[0].GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
		t.Fatalf("dependencies = %#v", dependencies)
	}
	dependency := dependencies[0]
	if dependency.GetSelectedSnapshot().GetSnapshotID() != snapshot.GetSnapshotID() || dependency.GetSelectedObservation().GetObservationID() != available.GetObservationID() || len(dependency.GetDependencyObservations()) != 2 {
		t.Fatalf("incomplete dependency proof = %#v", dependency)
	}
	if err := ValidateDependencySet(dependencies, now); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDependenciesRejectsDuplicateAndInternalRequirements(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	item := observation(key, now)
	snapshot := testSnapshot(now, item)
	if _, err := ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{key, key}, now); err == nil {
		t.Fatal("duplicate workload requirement was silently deduplicated")
	}
	internal := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER)
	if _, err := ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{internal}, now); err == nil {
		t.Fatal("internal fact was accepted as a workload dependency")
	}
}

func TestValidateSnapshotRejectsWrongCatalogOwner(t *testing.T) {
	now := time.Now().UTC()
	item := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER), now)
	item.Provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG
	NormalizeObservation(item)
	if err := ValidateSnapshot(testSnapshot(now, item), now); err == nil {
		t.Fatal("ValidateSnapshot() accepted a capability published by the wrong provider")
	}
}

func TestValidateSnapshotRejectsObservationAfterPublication(t *testing.T) {
	publishedAt := time.Now().UTC()
	item := observation(
		PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING),
		publishedAt.Add(time.Second),
	)
	if err := ValidateSnapshot(testSnapshot(publishedAt, item), publishedAt.Add(time.Second)); err == nil {
		t.Fatal("ValidateSnapshot() accepted an observation sampled after publication")
	}
}

func TestValidateEvidenceRejectsMalformedBootID(t *testing.T) {
	evidence := BootEvidence("not-a-boot-id")
	if err := ValidateEvidence(evidence, IdentityBoot); err == nil {
		t.Fatal("ValidateEvidence() accepted a non-canonical boot ID")
	}
}

func TestValidateEvidenceRequiresCanonicalSHA256Digest(t *testing.T) {
	evidence := ConfigEvidence(strings.Repeat("a", 64))
	if err := ValidateEvidence(evidence, IdentityConfig); err == nil {
		t.Fatal("ValidateEvidence() accepted a digest without the sha256 algorithm prefix")
	}
}

func TestValidateDependencySetRejectsMixedSnapshotReferences(t *testing.T) {
	now := time.Now().UTC()
	first := observation(ExtensionKey("example.com/first", "v1"), now)
	second := observation(ExtensionKey("example.com/second", "v1"), now)
	snapshot := testSnapshot(now, first, second)
	dependencies, err := ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{first.GetKey(), second.GetKey()}, now)
	if err != nil {
		t.Fatal(err)
	}
	dependencies[1].SelectedSnapshot = proto.Clone(dependencies[1].GetSelectedSnapshot()).(*capabilityv1.CapabilitySnapshotReference)
	dependencies[1].SelectedSnapshot.SnapshotID = "different-snapshot"
	if err := ValidateDependencySet(dependencies, now); err == nil {
		t.Fatal("ValidateDependencySet() accepted proofs selected from different snapshots")
	}
}

func TestValidateObservationProofRejectsOpaqueOrTamperedIdentity(t *testing.T) {
	now := time.Now().UTC()
	proof := NewObservationProof(observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING), now))
	proof.ObservationID = "sha256:" + strings.Repeat("b", 64)
	if err := ValidateObservationProof(proof, now); err == nil {
		t.Fatal("ValidateObservationProof() accepted an opaque observation ID")
	}

	proof = NewObservationProof(observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING), now))
	proof.ValidUntil = timestamppb.New(now.Add(10 * time.Second))
	if err := ValidateObservationProof(proof, now); err == nil {
		t.Fatal("ValidateObservationProof() accepted proof fields changed after observation ID creation")
	}
}

func TestValidateSnapshotRejectsContradictoryOrOversizedHealthMetadata(t *testing.T) {
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
		{name: "oversized reason", mutate: func(item *capabilityv1.CapabilityObservation) { item.Reason = strings.Repeat("x", MaxReasonBytes+1) }},
		{name: "unknown state enum", mutate: func(item *capabilityv1.CapabilityObservation) {
			item.State = capabilityv1.CapabilityState(99)
		}},
		{name: "unknown reason enum", mutate: func(item *capabilityv1.CapabilityObservation) {
			item.ReasonCode = capabilityv1.CapabilityReasonCode(99)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			item := observation(key, now)
			test.mutate(item)
			item.ObservationID = ObservationID(item)
			if err := ValidateSnapshot(testSnapshot(now, item), now); err == nil {
				t.Fatal("ValidateSnapshot() accepted contradictory health metadata")
			}
		})
	}
}

func TestValidateConditionSetRejectsUnknownEnums(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	for _, condition := range []*capabilityv1.CapabilityCondition{
		{Key: key, State: capabilityv1.CapabilityConditionState(99), ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED, ObservedAt: timestamppb.New(now)},
		{Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED, ReasonCode: capabilityv1.CapabilityReasonCode(99), ObservedAt: timestamppb.New(now)},
	} {
		set := &capabilityv1.CapabilityConditionSet{Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{condition}}
		if err := ValidateConditionSet(set, now); err == nil {
			t.Fatalf("ValidateConditionSet() accepted unknown enums: %#v", condition)
		}
	}
}

func TestValidateConditionSetRejectsInternalTimeInversion(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	proof := NewObservationProof(observation(key, now))

	t.Run("condition after set publication", func(t *testing.T) {
		condition := &capabilityv1.CapabilityCondition{
			Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			ObservedAt: timestamppb.New(now.Add(time.Second)), Proof: proof,
		}
		set := &capabilityv1.CapabilityConditionSet{Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{condition}}
		if err := ValidateConditionSet(set, now.Add(time.Second)); err == nil {
			t.Fatal("ValidateConditionSet() accepted a condition observed after set publication")
		}
	})

	t.Run("proof after condition", func(t *testing.T) {
		futureProof := NewObservationProof(observation(key, now.Add(time.Second)))
		condition := &capabilityv1.CapabilityCondition{
			Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			ObservedAt: timestamppb.New(now), Proof: futureProof,
		}
		set := &capabilityv1.CapabilityConditionSet{Revision: 1, ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{condition}}
		if err := ValidateConditionSet(set, now.Add(time.Second)); err == nil {
			t.Fatal("ValidateConditionSet() accepted proof sampled after its condition")
		}
	})
}

func TestDeriveRequirementsRejectsInternalInjectionAndUsesRuntimeSpecificCapabilities(t *testing.T) {
	keys, err := DeriveRequirements(RequirementInput{
		RuntimeName: "runsc", HasPorts: true, NetworkMode: "default", NetworkBackend: "ebpf",
		MemoryLimitBytes: 1, RootfsWritable: true, EROFSBacking: true,
		ExtensionCapabilityRequests: []*capabilityv1.ExtensionCapabilityRequirement{{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/gpu", Value: "a100"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS,
	} {
		if !containsPlatform(keys, required) {
			t.Fatalf("requirements %#v do not contain %s", keys, required)
		}
	}
	if err := ValidateRequirementKeys([]*capabilityv1.CapabilityKey{PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA)}); err == nil {
		t.Fatal("internal fact injection was accepted")
	}
}

func TestDeriveRequirementsRejectsNonHostNetworkWithoutObservedBackend(t *testing.T) {
	_, err := DeriveRequirements(RequirementInput{RuntimeName: "runsc", NetworkMode: "default"})
	if err == nil || !strings.Contains(err.Error(), "observed network backend") {
		t.Fatalf("missing backend error = %v", err)
	}
	if _, err := DeriveRequirements(RequirementInput{RuntimeName: "runsc", NetworkMode: "host"}); err != nil {
		t.Fatalf("host network unexpectedly requires a backend: %v", err)
	}
	static, err := DeriveRequestStaticRequirements(RequirementInput{RuntimeName: "runsc", NetworkMode: "default", HasPorts: true})
	if err != nil {
		t.Fatalf("derive request-static requirements: %v", err)
	}
	if !containsPlatform(static, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING) ||
		containsPlatform(static, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE) ||
		containsPlatform(static, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET) {
		t.Fatalf("request-static requirements = %#v", static)
	}
}

func TestEvaluateObservationTransitionIgnoresTTLRefreshButIncludesReasonCode(t *testing.T) {
	now := time.Now().UTC()
	previous := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING), now)
	current := observation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING), now.Add(5*time.Second))
	previousSnapshot := testSnapshot(now, previous)
	currentSnapshot := &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance", Sequence: 2, SnapshotID: "snapshot-2",
		CollectedAt: timestamppb.New(now.Add(5 * time.Second)), Observations: []*capabilityv1.CapabilityObservation{current},
	}
	if _, changed := EvaluateObservationTransition(previousSnapshot, previous, now, currentSnapshot, current, now.Add(5*time.Second)); changed {
		t.Fatal("ordinary TTL refresh created a transition")
	}
	current.State = capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED
	current.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	NormalizeObservation(current)
	if _, changed := EvaluateObservationTransition(previousSnapshot, previous, now, currentSnapshot, current, now.Add(5*time.Second)); !changed {
		t.Fatal("state/reason change did not create a transition")
	}
}

func TestEvaluateObservationTransitionNormalizesExpiryAndAbsence(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	available := observation(key, now)
	previous := testSnapshot(now, available)
	current := proto.Clone(previous).(*capabilityv1.CapabilitySnapshot)
	current.Sequence++
	current.SnapshotID = "snapshot-2"
	current.CollectedAt = timestamppb.New(available.GetValidUntil().AsTime().Add(time.Second))

	evaluation, changed := EvaluateObservationTransition(
		previous, available, now,
		current, current.GetObservations()[0], current.GetCollectedAt().AsTime(),
	)
	if !changed || evaluation.Previous.State != capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE ||
		evaluation.Current.State != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN ||
		evaluation.Current.ReasonCode != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED {
		t.Fatalf("expiry evaluation = (%+v, %t)", evaluation, changed)
	}

	missing, changed := EvaluateObservationTransition(
		previous, available, now,
		&capabilityv1.CapabilitySnapshot{CollectedAt: timestamppb.New(now.Add(time.Second))}, nil, now.Add(time.Second),
	)
	if !changed || missing.Current.State != capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN ||
		missing.Current.ReasonCode != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE ||
		missing.Current.Reason == "" {
		t.Fatalf("missing observation evaluation = (%+v, %t)", missing, changed)
	}
}

func observation(key *capabilityv1.CapabilityKey, now time.Time) *capabilityv1.CapabilityObservation {
	provider, identity, err := ObservationOwner(key)
	if err != nil {
		panic(err)
	}
	var evidence *capabilityv1.CapabilityEvidence
	switch identity {
	case IdentityConfig:
		evidence = ConfigEvidence(testDigest("config"))
	case IdentityBoot:
		evidence = BootEvidence(testBootID)
	case IdentityMount:
		evidence = MountEvidence(testBootID, "1:source:/filestore")
	case IdentityRuntime:
		runtimeName := "runc"
		if strings.Contains(strings.ToLower(key.GetPlatform().String()), "runsc") {
			runtimeName = "runsc"
		}
		evidence = RuntimeEvidence(testBootID, runtimeName, testDigest("binary"), testDigest("runtime-config"))
	case IdentityDerived:
		evidence = DerivedEvidence()
	default:
		panic("unsupported identity")
	}
	result := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider: provider, ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		ObservedAt: timestamppb.New(now), Evidence: evidence,
	}
	if key.GetExtension() == nil {
		definition, _ := PlatformDefinition(key.GetPlatform())
		if definition.Freshness.MaxValidity > 0 {
			result.ValidUntil = timestamppb.New(now.Add(definition.Freshness.MaxValidity))
		}
	}
	NormalizeObservation(result)
	return result
}

func derivedObservation(key *capabilityv1.CapabilityKey, now time.Time, dependencies ...*capabilityv1.CapabilityObservation) *capabilityv1.CapabilityObservation {
	result := observation(key, now)
	result.Dependencies = make([]*capabilityv1.CapabilityObservationProof, 0, len(dependencies))
	for _, dependency := range dependencies {
		result.Dependencies = append(result.Dependencies, NewObservationProof(dependency))
	}
	result.Evidence = DerivedEvidence(result.Dependencies...)
	if expiry := earliestExpiry(result.Dependencies); expiry != nil {
		result.ValidUntil = timestamppb.New(*expiry)
	}
	NormalizeObservation(result)
	return result
}

func testSnapshot(collectedAt time.Time, observations ...*capabilityv1.CapabilityObservation) *capabilityv1.CapabilitySnapshot {
	return &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 1, SnapshotID: "snapshot", CollectedAt: timestamppb.New(collectedAt), Observations: observations}
}

func testDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func containsPlatform(keys []*capabilityv1.CapabilityKey, platform capabilityv1.PlatformCapability) bool {
	for _, key := range keys {
		if key.GetPlatform() == platform {
			return true
		}
	}
	return false
}
