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
}

func TestValidateExtensionRejectsInvalidValues(t *testing.T) {
	for _, capability := range []*capabilityv1.ExtensionCapability{
		{Name: "gpu"}, {Name: "axern.io/gpu"}, {Name: "internal.axern.io/gpu"},
		{Name: "axern.dev/feature"}, {Name: "bad_domain/gpu"}, {Name: " example.com/gpu"},
		{Name: "example.com/gpu "}, {Name: "example.com/gpu", Value: string([]byte{0xff})},
		{Name: "example.com/gpu", Value: strings.Repeat("x", MaxExtensionValueBytes+1)},
	} {
		if err := ValidateExtension(capability); err == nil {
			t.Fatalf("ValidateExtension(%q) succeeded", capability.GetName())
		}
	}
	if err := ValidateExtension(&capabilityv1.ExtensionCapability{Name: "example.com/gpu", Value: "a100"}); err != nil {
		t.Fatal(err)
	}
}

func TestExtensionRequirementIdentityIsExactAndUnique(t *testing.T) {
	left := ExtensionKey("Example.COM/Accelerator", " exact value ")
	if left.GetExtension().GetName() != "example.com/Accelerator" || left.GetExtension().GetValue() != " exact value " {
		t.Fatalf("normalized extension = %#v", left.GetExtension())
	}
	leftID, _ := KeyID(left)
	rightID, _ := KeyID(ExtensionKey("example.com/Accelerator", "exact value"))
	if leftID == rightID || strings.ContainsRune(leftID, '\x00') {
		t.Fatalf("extension identities are not exact and safe: %q %q", leftID, rightID)
	}
	requirements := []*capabilityv1.ExtensionCapabilityRequirement{
		{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "a100"}},
		{Capability: &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "h100"}},
	}
	if err := ValidateExtensionRequirements(requirements); err == nil {
		t.Fatal("multiple values for one extension name were accepted")
	}
}

func TestBoundedReasonProducesValidUTF8WithinByteLimit(t *testing.T) {
	bounded := BoundedReason(string([]byte{0xff}) + strings.Repeat("界", MaxReasonBytes))
	if !utf8.ValidString(bounded) || len(bounded) > MaxReasonBytes {
		t.Fatalf("invalid bounded reason length=%d", len(bounded))
	}
}

func TestCatalogRejectsInvalidDependencyAndLossPolicy(t *testing.T) {
	cloneCatalog := func() map[capabilityv1.PlatformCapability]Definition {
		out := make(map[capabilityv1.PlatformCapability]Definition, len(definitions))
		for key, definition := range definitions {
			definition.Dependencies = append([]capabilityv1.PlatformCapability(nil), definition.Dependencies...)
			out[key] = definition
		}
		return out
	}
	catalog := cloneCatalog()
	derived := catalog[capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT]
	derived.Dependencies = []capabilityv1.PlatformCapability{derived.Key}
	catalog[derived.Key] = derived
	if err := validateCatalog(catalog); err == nil {
		t.Fatal("catalog accepted a dependency cycle")
	}
	catalog = cloneCatalog()
	withoutVerifier := catalog[capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS]
	withoutVerifier.LossPolicy = capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP
	catalog[withoutVerifier.Key] = withoutVerifier
	if err := validateCatalog(catalog); err == nil {
		t.Fatal("catalog accepted fail-stop without an allocation verifier")
	}
}

func TestAvailableObservationUsesCatalogDependenciesAndFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	baseKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTestKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	derivedKey := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	base := testObservation(baseKey, now)
	selfTest := testObservation(selfTestKey, now)
	derived := testObservation(derivedKey, now)
	snapshot := testSnapshot(now, base, selfTest, derived)
	if _, ok := AvailableObservation(snapshot, derivedKey, now); !ok {
		t.Fatal("available derived requirement was rejected")
	}
	base.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	base.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	if _, ok := AvailableObservation(snapshot, derivedKey, now); ok {
		t.Fatal("derived requirement survived an unavailable catalog dependency")
	}
	base = testObservation(baseKey, now)
	snapshot = testSnapshot(now.Add(16*time.Second), base, selfTest, derived)
	if _, ok := AvailableObservation(snapshot, derivedKey, now.Add(16*time.Second)); ok {
		t.Fatal("derived requirement survived an expired dependency")
	}
}

func TestResolveRequirementsReturnsOnlyImmutableSpecification(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT)
	snapshot := testSnapshot(now,
		testObservation(key, now),
		testObservation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER), now),
		testObservation(PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST), now),
	)
	requirements, err := ResolveRequirements(snapshot, []*capabilityv1.CapabilityKey{key}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 1 || !proto.Equal(requirements[0].GetKey(), key) || requirements[0].GetLossPolicy() != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP {
		t.Fatalf("requirements = %#v", requirements)
	}
	if err := ValidateRequirements(requirements); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRequirementsRejectsDuplicateInternalAndUnavailable(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	snapshot := testSnapshot(now, testObservation(key, now))
	if _, err := ResolveRequirements(snapshot, []*capabilityv1.CapabilityKey{key, key}, now); err == nil {
		t.Fatal("duplicate requirement was accepted")
	}
	internal := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER)
	if _, err := ResolveRequirements(snapshot, []*capabilityv1.CapabilityKey{internal}, now); err == nil {
		t.Fatal("internal fact was accepted as a workload requirement")
	}
	if _, err := ResolveRequirements(snapshot, []*capabilityv1.CapabilityKey{PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET)}, now); err == nil {
		t.Fatal("unavailable requirement was accepted")
	}
}

func TestValidateSnapshotRejectsOrderingAndMalformedFacts(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	wrongOwner := testObservation(key, now)
	wrongOwner.Provider = capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG
	if err := ValidateSnapshot(testSnapshot(now, wrongOwner), now); err == nil {
		t.Fatal("wrong provider owner was accepted")
	}
	future := testObservation(key, now.Add(time.Second))
	if err := ValidateSnapshot(testSnapshot(now, future), now.Add(time.Second)); err == nil {
		t.Fatal("observation after snapshot publication was accepted")
	}
	if err := ValidateSnapshot(&capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 0, CollectedAt: timestamppb.New(now)}, now); err == nil {
		t.Fatal("non-positive snapshot sequence was accepted")
	}
}

func TestValidateEvidenceRejectsMalformedHostIdentity(t *testing.T) {
	if err := ValidateEvidence(BootEvidence("not-a-boot-id"), IdentityBoot); err == nil {
		t.Fatal("malformed boot identity was accepted")
	}
	if err := ValidateEvidence(RuntimeEvidence(testBootID, strings.Repeat("a", 64), testDigest("config")), IdentityRuntime); err == nil {
		t.Fatal("runtime digest without sha256 prefix was accepted")
	}
	if err := ValidateEvidence(nil, IdentityConfig); err != nil {
		t.Fatalf("configuration observation requires synthetic evidence: %v", err)
	}
}

func TestValidateConditionSetUsesOneProjectionTimestamp(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	valid := &capabilityv1.CapabilityConditionSet{ObservedAt: timestamppb.New(now), Conditions: []*capabilityv1.CapabilityCondition{{
		Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED,
	}}}
	if err := ValidateConditionSet(valid, now); err != nil {
		t.Fatal(err)
	}
	invalid := proto.Clone(valid).(*capabilityv1.CapabilityConditionSet)
	invalid.Conditions[0].State = capabilityv1.CapabilityConditionState(99)
	if err := ValidateConditionSet(invalid, now); err == nil {
		t.Fatal("unknown condition state was accepted")
	}
	invalid = proto.Clone(valid).(*capabilityv1.CapabilityConditionSet)
	invalid.ObservedAt = timestamppb.New(now.Add(2 * time.Minute))
	if err := ValidateConditionSet(invalid, now); err == nil {
		t.Fatal("future condition projection was accepted")
	}
}

func TestDeriveRequirementsUsesExecutionCapabilities(t *testing.T) {
	keys, err := DeriveRequirements(RequirementInput{
		HasPorts: true, NetworkMode: "default", NetworkBackend: "ebpf",
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
			t.Fatalf("requirements do not contain %s", required)
		}
	}
	if _, err := DeriveRequirements(RequirementInput{NetworkMode: "default"}); err == nil {
		t.Fatal("non-host network without an observed backend was accepted")
	}
}

func TestEvaluateObservationTransitionIgnoresRefreshAndDetectsStateChange(t *testing.T) {
	now := time.Now().UTC()
	key := PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	previousObservation := testObservation(key, now)
	currentObservation := testObservation(key, now.Add(5*time.Second))
	previous := testSnapshot(now, previousObservation)
	current := &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 2, CollectedAt: timestamppb.New(now.Add(5 * time.Second)), Observations: []*capabilityv1.CapabilityObservation{currentObservation}}
	if _, changed := EvaluateObservationTransition(previous, previousObservation, now, current, currentObservation, now.Add(5*time.Second)); changed {
		t.Fatal("ordinary refresh created a transition")
	}
	currentObservation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_DEGRADED
	currentObservation.ReasonCode = capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED
	if _, changed := EvaluateObservationTransition(previous, previousObservation, now, current, currentObservation, now.Add(5*time.Second)); !changed {
		t.Fatal("state change did not create a transition")
	}
}

func testObservation(key *capabilityv1.CapabilityKey, now time.Time) *capabilityv1.CapabilityObservation {
	provider, identity, err := ObservationOwner(key)
	if err != nil {
		panic(err)
	}
	var evidence *capabilityv1.CapabilityEvidence
	switch identity {
	case IdentityConfig, IdentityDerived:
	case IdentityBoot:
		evidence = BootEvidence(testBootID)
	case IdentityMount:
		evidence = MountEvidence(testBootID, "1:source:/filestore")
	case IdentityRuntime:
		evidence = RuntimeEvidence(testBootID, testDigest("binary"), testDigest("runtime-config"))
	default:
		panic("unsupported identity")
	}
	observation := &capabilityv1.CapabilityObservation{
		Key: key, State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE, Provider: provider,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		ObservedAt: timestamppb.New(now), Evidence: evidence,
	}
	if key.GetExtension() == nil {
		definition, _ := PlatformDefinition(key.GetPlatform())
		if definition.Freshness.MaxValidity > 0 {
			observation.ValidUntil = timestamppb.New(now.Add(definition.Freshness.MaxValidity))
		}
	}
	NormalizeObservation(observation)
	return observation
}

func testSnapshot(collectedAt time.Time, observations ...*capabilityv1.CapabilityObservation) *capabilityv1.CapabilitySnapshot {
	return &capabilityv1.CapabilitySnapshot{NodeInstanceID: "instance", Sequence: 1, CollectedAt: timestamppb.New(collectedAt), Observations: observations}
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
