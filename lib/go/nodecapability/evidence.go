package nodecapability

import (
	"fmt"
	"regexp"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	boundedIdentityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/+=,@-]{0,511}$`)
	digestPattern          = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	bootIDPattern          = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
)

func BootEvidence(bootID string) *capabilityv1.CapabilityEvidence {
	return &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Boot{Boot: &capabilityv1.BootEvidenceIdentity{BootID: bootID}}}
}

func MountEvidence(bootID, mountIdentity string) *capabilityv1.CapabilityEvidence {
	return &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Mount{Mount: &capabilityv1.MountEvidenceIdentity{BootID: bootID, MountIdentity: mountIdentity}}}
}

func RuntimeEvidence(bootID, binaryDigest, configDigest string) *capabilityv1.CapabilityEvidence {
	return &capabilityv1.CapabilityEvidence{Identity: &capabilityv1.CapabilityEvidence_Runtime{Runtime: &capabilityv1.RuntimeEvidenceIdentity{BootID: bootID, RuntimeBinaryDigest: binaryDigest, RuntimeConfigDigest: configDigest}}}
}

func EvidenceIdentityKind(evidence *capabilityv1.CapabilityEvidence) IdentityKind {
	if evidence == nil {
		return IdentityUnspecified
	}
	switch evidence.GetIdentity().(type) {
	case *capabilityv1.CapabilityEvidence_Boot:
		return IdentityBoot
	case *capabilityv1.CapabilityEvidence_Mount:
		return IdentityMount
	case *capabilityv1.CapabilityEvidence_Runtime:
		return IdentityRuntime
	default:
		return IdentityUnspecified
	}
}

func ValidateEvidence(evidence *capabilityv1.CapabilityEvidence, expected IdentityKind) error {
	if expected == IdentityConfig || expected == IdentityDerived {
		if evidence != nil {
			return fmt.Errorf("configuration and derived capabilities must not carry synthetic evidence")
		}
		return nil
	}
	if evidence == nil || evidence.GetIdentity() == nil {
		return fmt.Errorf("typed host evidence is required")
	}
	if kind := EvidenceIdentityKind(evidence); kind != expected {
		return fmt.Errorf("evidence kind %d does not match catalog kind %d", kind, expected)
	}
	switch identity := evidence.GetIdentity().(type) {
	case *capabilityv1.CapabilityEvidence_Boot:
		return validateBootID(identity.Boot.GetBootID())
	case *capabilityv1.CapabilityEvidence_Mount:
		if err := validateBootID(identity.Mount.GetBootID()); err != nil {
			return err
		}
		return validateBoundedIdentity("mount_identity", identity.Mount.GetMountIdentity())
	case *capabilityv1.CapabilityEvidence_Runtime:
		if err := validateBootID(identity.Runtime.GetBootID()); err != nil {
			return err
		}
		if !digestPattern.MatchString(identity.Runtime.GetRuntimeBinaryDigest()) {
			return fmt.Errorf("runtime_binary_digest must be a sha256 digest")
		}
		if !digestPattern.MatchString(identity.Runtime.GetRuntimeConfigDigest()) {
			return fmt.Errorf("runtime_config_digest must be a sha256 digest")
		}
		return nil
	default:
		return fmt.Errorf("unsupported capability evidence")
	}
}

func validateBootID(value string) error {
	if !bootIDPattern.MatchString(value) {
		return fmt.Errorf("boot_id must be a canonical lowercase UUID")
	}
	return nil
}

func validateFreshness(key *capabilityv1.CapabilityKey, observedAt, validUntil *timestamppb.Timestamp, now time.Time, requireCurrent bool) error {
	if observedAt == nil {
		return fmt.Errorf("observed_at is required")
	}
	var maxValidity time.Duration
	if key.GetExtension() == nil {
		definition, ok := PlatformDefinition(key.GetPlatform())
		if !ok {
			return fmt.Errorf("unknown platform capability %d", key.GetPlatform())
		}
		maxValidity = definition.Freshness.MaxValidity
	}
	if maxValidity == 0 {
		if validUntil != nil {
			return fmt.Errorf("non-expiring observation cannot set valid_until")
		}
		return nil
	}
	if validUntil == nil {
		return fmt.Errorf("expiring observation is missing valid_until")
	}
	observed, expires := observedAt.AsTime(), validUntil.AsTime()
	if !expires.After(observed) || expires.After(observed.Add(maxValidity)) {
		return fmt.Errorf("valid_until is outside the catalog freshness bound")
	}
	if requireCurrent && !expires.After(now) {
		return fmt.Errorf("capability observation is expired")
	}
	return nil
}
