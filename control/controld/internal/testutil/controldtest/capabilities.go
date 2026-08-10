package controldtest

import (
	"sort"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBootID = "11111111-2222-3333-4444-555555555555"
)

// AvailableCapabilitySnapshot builds catalog-valid typed observations for
// control-plane tests, including every transitive internal proof.
func AvailableCapabilitySnapshot(observedAt time.Time, platforms ...capabilityv1.PlatformCapability) *capabilityv1.CapabilitySnapshot {
	byPlatform := make(map[capabilityv1.PlatformCapability]*capabilityv1.CapabilityObservation)
	var add func(capabilityv1.PlatformCapability) *capabilityv1.CapabilityObservation
	add = func(platform capabilityv1.PlatformCapability) *capabilityv1.CapabilityObservation {
		if existing := byPlatform[platform]; existing != nil {
			return existing
		}
		definition, ok := capabilitycontract.PlatformDefinition(platform)
		if !ok {
			panic("unknown test platform capability")
		}
		var evidence *capabilityv1.CapabilityEvidence
		switch definition.Identity {
		case capabilitycontract.IdentityConfig:
			evidence = capabilitycontract.ConfigEvidence(testDigest)
		case capabilitycontract.IdentityBoot:
			evidence = capabilitycontract.BootEvidence(testBootID)
		case capabilitycontract.IdentityMount:
			evidence = capabilitycontract.MountEvidence(testBootID, "42:/test:xfs")
		case capabilitycontract.IdentityRuntime:
			runtimeName := "runc"
			if strings.Contains(platform.String(), "RUNSC") {
				runtimeName = "runsc"
			}
			evidence = capabilitycontract.RuntimeEvidence(testBootID, runtimeName, testDigest, testDigest)
		case capabilitycontract.IdentityDerived:
			// Derived evidence is assigned after its dependency proof set has
			// been constructed below.
		default:
			panic("unsupported test capability identity")
		}
		observation := &capabilityv1.CapabilityObservation{
			Key: capabilitycontract.PlatformKey(platform), State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			Provider: definition.Provider, ObservedAt: timestamppb.New(observedAt), Evidence: evidence,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		}
		byPlatform[platform] = observation
		var earliest time.Time
		for _, dependencyPlatform := range definition.Dependencies {
			dependency := add(dependencyPlatform)
			proof := capabilitycontract.NewObservationProof(dependency)
			observation.Dependencies = append(observation.Dependencies, proof)
			if proof.GetValidUntil() != nil && (earliest.IsZero() || proof.GetValidUntil().AsTime().Before(earliest)) {
				earliest = proof.GetValidUntil().AsTime()
			}
		}
		if definition.Identity == capabilitycontract.IdentityDerived {
			observation.Evidence = capabilitycontract.DerivedEvidence(observation.GetDependencies()...)
		}
		if definition.Freshness.MaxValidity > 0 {
			observation.ValidUntil = timestamppb.New(observedAt.Add(definition.Freshness.MaxValidity))
		} else if definition.Identity == capabilitycontract.IdentityDerived && !earliest.IsZero() {
			observation.ValidUntil = timestamppb.New(earliest)
		}
		capabilitycontract.NormalizeObservation(observation)
		return observation
	}
	for _, platform := range platforms {
		add(platform)
	}
	observations := make([]*capabilityv1.CapabilityObservation, 0, len(byPlatform))
	for _, observation := range byPlatform {
		observations = append(observations, observation)
	}
	sort.Slice(observations, func(i, j int) bool {
		left, _ := capabilitycontract.KeyID(observations[i].GetKey())
		right, _ := capabilitycontract.KeyID(observations[j].GetKey())
		return left < right
	})
	return &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "test-node-instance", Sequence: 1, SnapshotID: "test-snapshot",
		CollectedAt: timestamppb.New(observedAt), Observations: observations,
	}
}
