package controldtest

import (
	"sort"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBootID = "11111111-2222-3333-4444-555555555555"
)

// AvailableCapabilitySnapshot builds contract-valid typed observations for
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
			evidence = nil
		case capabilitycontract.IdentityBoot:
			evidence = capabilitycontract.BootEvidence(testBootID)
		case capabilitycontract.IdentityMount:
			evidence = capabilitycontract.MountEvidence(testBootID, "42:/test:xfs")
		case capabilitycontract.IdentityRuntime:
			evidence = capabilitycontract.RuntimeEvidence(testBootID, testDigest, testDigest)
		case capabilitycontract.IdentityDerived:
		default:
			panic("unsupported test capability identity")
		}
		observation := &capabilityv1.CapabilityObservation{
			Key: capabilitycontract.PlatformKey(platform), State: capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			Provider: definition.Provider, ObservedAt: timestamppb.New(observedAt), Evidence: evidence,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
		}
		byPlatform[platform] = observation
		for _, dependencyPlatform := range definition.Dependencies {
			add(dependencyPlatform)
		}
		if definition.Freshness.MaxValidity > 0 {
			observation.ValidUntil = timestamppb.New(observedAt.Add(definition.Freshness.MaxValidity))
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
		CollectedAt: timestamppb.New(observedAt), Observations: observations,
	}
}
