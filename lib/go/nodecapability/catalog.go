// Package nodecapability owns the canonical node capability catalog and the
// pure validation rules shared by axnoded and controld.
package nodecapability

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

// IdentityKind describes the concrete subject an observation proves. It is
// intentionally independent from Freshness: a mount or runtime has a stable
// identity and can still require periodic revalidation.
type IdentityKind uint8

const (
	IdentityUnspecified IdentityKind = iota
	IdentityConfig
	IdentityBoot
	IdentityMount
	IdentityRuntime
	IdentityDerived
)

// Audience prevents implementation facts from leaking into the workload
// admission contract.
type Audience uint8

const (
	AudienceUnspecified Audience = iota
	AudienceInternalFact
	AudienceWorkloadRequirement
)

// AllocationVerifier names the allocation-specific enforcement check used
// when a node-level observation changes.
type AllocationVerifier uint8

const (
	VerifierNone AllocationVerifier = iota
	VerifierNetwork
	VerifierMemoryHardLimit
	VerifierRuncEphemeralStorage
	VerifierRunscEphemeralStorage
	VerifierEgressPolicy
)

type FreshnessPolicy struct {
	// MaxValidity is zero for identity-scoped facts that do not expire. A
	// positive value requires valid_until and bounds it from observed_at.
	MaxValidity time.Duration
}

type Definition struct {
	Key          capabilityv1.PlatformCapability
	Provider     capabilityv1.CapabilityProvider
	Identity     IdentityKind
	Freshness    FreshnessPolicy
	LossPolicy   capabilityv1.CapabilityLossPolicy
	Audience     Audience
	Verifier     AllocationVerifier
	Dependencies []capabilityv1.PlatformCapability
}

const (
	HealthObservationValidity = 15 * time.Second
)

var definitions = mustCatalog(map[capabilityv1.PlatformCapability]Definition{
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING:                       platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, IdentityConfig, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE, AudienceWorkloadRequirement, VerifierNetwork),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE:                        platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, IdentityConfig, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE, AudienceWorkloadRequirement, VerifierNetwork),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET:                        platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, IdentityConfig, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE, AudienceWorkloadRequirement, VerifierNetwork),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER:           platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP, IdentityBoot, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT:                platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, IdentityDerived, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, AudienceWorkloadRequirement, VerifierMemoryHardLimit, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT:               platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, IdentityDerived, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, AudienceWorkloadRequirement, VerifierMemoryHardLimit, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER:             platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, IdentityMount, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA:                     platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, IdentityMount, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT:     platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, IdentityDerived, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, AudienceWorkloadRequirement, VerifierRuncEphemeralStorage, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT:    platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, IdentityDerived, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, AudienceWorkloadRequirement, VerifierRunscEphemeralStorage, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS:                    platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, IdentityMount, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceWorkloadRequirement, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST:     platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST, IdentityRuntime, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST:    platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, IdentityRuntime, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST:  platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST, IdentityRuntime, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST: platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, IdentityRuntime, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST:          platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, IdentityConfig, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_STRICT_EGRESS_SELF_TEST:       platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_STRICT_EGRESS_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, IdentityConfig, HealthObservationValidity, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, AudienceInternalFact, VerifierNone),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT:                platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, IdentityDerived, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, AudienceWorkloadRequirement, VerifierEgressPolicy, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT:             platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, IdentityDerived, 0, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, AudienceWorkloadRequirement, VerifierEgressPolicy, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_DNS_POLICY_SELF_TEST, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_EGRESSD_STRICT_EGRESS_SELF_TEST),
})

func platformDefinition(key capabilityv1.PlatformCapability, provider capabilityv1.CapabilityProvider, identity IdentityKind, validity time.Duration, lossPolicy capabilityv1.CapabilityLossPolicy, audience Audience, verifier AllocationVerifier, dependencies ...capabilityv1.PlatformCapability) Definition {
	return Definition{Key: key, Provider: provider, Identity: identity, Freshness: FreshnessPolicy{MaxValidity: validity}, LossPolicy: lossPolicy, Audience: audience, Verifier: verifier, Dependencies: append([]capabilityv1.PlatformCapability(nil), dependencies...)}
}

func mustCatalog(in map[capabilityv1.PlatformCapability]Definition) map[capabilityv1.PlatformCapability]Definition {
	if err := validateCatalog(in); err != nil {
		panic("invalid capability catalog: " + err.Error())
	}
	return in
}

func validateCatalog(in map[capabilityv1.PlatformCapability]Definition) error {
	for number, name := range capabilityv1.PlatformCapability_name {
		key := capabilityv1.PlatformCapability(number)
		if key == capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_UNSPECIFIED {
			continue
		}
		definition, ok := in[key]
		if !ok {
			return fmt.Errorf("enum %s is not defined", name)
		}
		if definition.Key != key || definition.Provider == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_UNSPECIFIED || definition.Identity == IdentityUnspecified || definition.Audience == AudienceUnspecified || definition.LossPolicy == capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_UNSPECIFIED {
			return fmt.Errorf("definition %s is incomplete", name)
		}
		if _, known := capabilityv1.CapabilityProvider_name[int32(definition.Provider)]; !known {
			return fmt.Errorf("definition %s has unknown provider %d", name, definition.Provider)
		}
		if definition.Audience == AudienceInternalFact && (definition.LossPolicy != capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY || definition.Verifier != VerifierNone) {
			return fmt.Errorf("internal fact %s must be admission-only and cannot own an allocation verifier", name)
		}
		switch definition.LossPolicy {
		case capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY:
			if definition.Verifier != VerifierNone {
				return fmt.Errorf("admission-only definition %s cannot own an allocation verifier", name)
			}
		case capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE,
			capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP:
			if definition.Verifier == VerifierNone {
				return fmt.Errorf("runtime loss definition %s requires an allocation verifier", name)
			}
		default:
			return fmt.Errorf("definition %s has unknown loss policy %d", name, definition.LossPolicy)
		}
		if definition.Provider == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED {
			if definition.Identity != IdentityDerived || definition.Audience != AudienceWorkloadRequirement || len(definition.Dependencies) == 0 {
				return fmt.Errorf("derived definition %s requires derived identity, workload audience, and dependencies", name)
			}
		} else if len(definition.Dependencies) != 0 || definition.Identity == IdentityDerived {
			return fmt.Errorf("base definition %s cannot declare derived metadata", name)
		}
		for _, dependency := range definition.Dependencies {
			dep, ok := in[dependency]
			if !ok {
				return fmt.Errorf("definition %s has unknown dependency %d", name, dependency)
			}
			if dep.Audience != AudienceInternalFact {
				return fmt.Errorf("derived definition %s may depend only on internal facts", name)
			}
			if dep.Provider == capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED {
				return fmt.Errorf("derived definition %s may consume only base observations", name)
			}
		}
	}
	visiting := make(map[capabilityv1.PlatformCapability]bool)
	visited := make(map[capabilityv1.PlatformCapability]bool)
	var visit func(capabilityv1.PlatformCapability) error
	visit = func(key capabilityv1.PlatformCapability) error {
		if visiting[key] {
			return fmt.Errorf("dependency cycle at %s", key)
		}
		if visited[key] {
			return nil
		}
		visiting[key] = true
		for _, dependency := range in[key].Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, key)
		visited[key] = true
		return nil
	}
	for key := range in {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func PlatformDefinition(key capabilityv1.PlatformCapability) (Definition, bool) {
	definition, ok := definitions[key]
	definition.Dependencies = append([]capabilityv1.PlatformCapability(nil), definition.Dependencies...)
	return definition, ok
}

func PlatformDefinitions() []Definition {
	out := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		definition.Dependencies = append([]capabilityv1.PlatformCapability(nil), definition.Dependencies...)
		out = append(out, definition)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// CatalogDigest binds derived evidence to the exact static catalog semantics
// used to evaluate its bounded direct dependency set.
func CatalogDigest() string {
	hash := sha256.New()
	for _, definition := range PlatformDefinitions() {
		_, _ = fmt.Fprintf(hash, "%d|%d|%d|%d|%d|%d|%d", definition.Key, definition.Provider, definition.Identity, definition.Freshness.MaxValidity, definition.LossPolicy, definition.Audience, definition.Verifier)
		for _, dependency := range definition.Dependencies {
			_, _ = fmt.Fprintf(hash, "|%d", dependency)
		}
		_, _ = hash.Write([]byte{'\n'})
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func PlatformDependencyKeys(key capabilityv1.PlatformCapability) ([]*capabilityv1.CapabilityKey, error) {
	definition, ok := PlatformDefinition(key)
	if !ok {
		return nil, fmt.Errorf("unknown platform capability %d", key)
	}
	keys := make([]*capabilityv1.CapabilityKey, 0, len(definition.Dependencies))
	for _, dependency := range definition.Dependencies {
		keys = append(keys, PlatformKey(dependency))
	}
	return keys, nil
}

func ObservationOwner(key *capabilityv1.CapabilityKey) (capabilityv1.CapabilityProvider, IdentityKind, error) {
	if key == nil {
		return 0, 0, fmt.Errorf("capability key is required")
	}
	if extension := key.GetExtension(); extension != nil {
		if err := ValidateExtension(extension); err != nil {
			return 0, 0, err
		}
		return capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG, IdentityConfig, nil
	}
	definition, ok := PlatformDefinition(key.GetPlatform())
	if !ok {
		return 0, 0, fmt.Errorf("unknown platform capability %d", key.GetPlatform())
	}
	return definition.Provider, definition.Identity, nil
}

func PlatformKey(key capabilityv1.PlatformCapability) *capabilityv1.CapabilityKey {
	return &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{Platform: key}}
}

func CloneKey(key *capabilityv1.CapabilityKey) *capabilityv1.CapabilityKey {
	if key == nil {
		return nil
	}
	if key.GetExtension() != nil {
		return ExtensionKey(key.GetExtension().GetName(), key.GetExtension().GetValue())
	}
	return PlatformKey(key.GetPlatform())
}

// MetricKey returns a bounded label value. Extension names are intentionally
// collapsed so user-controlled qualified names cannot create metric series.
func MetricKey(key *capabilityv1.CapabilityKey) string {
	if key == nil {
		return "unknown"
	}
	if key.GetExtension() != nil {
		return "extension"
	}
	name := strings.ToLower(key.GetPlatform().String())
	name = strings.TrimPrefix(name, "platform_capability_")
	if name == "" || name == "unspecified" {
		return "unknown"
	}
	return name
}

func ExtensionKey(name, value string) *capabilityv1.CapabilityKey {
	return &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Extension{Extension: NormalizeExtension(&capabilityv1.ExtensionCapability{Name: name, Value: value})}}
}

// KeyID is an internal comparison and persistence key. It is deliberately
// derived from typed fields rather than accepted as an external capability
// name.
func KeyID(key *capabilityv1.CapabilityKey) (string, error) {
	if key == nil {
		return "", fmt.Errorf("capability key is required")
	}
	if platform := key.GetPlatform(); platform != capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_UNSPECIFIED {
		if _, ok := PlatformDefinition(platform); !ok {
			return "", fmt.Errorf("unknown platform capability %d", platform)
		}
		return fmt.Sprintf("platform/%d", platform), nil
	}
	extension := key.GetExtension()
	if extension == nil {
		return "", fmt.Errorf("capability key kind is required")
	}
	if err := ValidateExtension(extension); err != nil {
		return "", err
	}
	normalized := NormalizeExtension(extension)
	return "extension/" + normalized.GetName() + "/" + base64.RawURLEncoding.EncodeToString([]byte(normalized.GetValue())), nil
}

func LossPolicy(key *capabilityv1.CapabilityKey) (capabilityv1.CapabilityLossPolicy, error) {
	if key == nil {
		return capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_UNSPECIFIED, fmt.Errorf("capability key is required")
	}
	if extension := key.GetExtension(); extension != nil {
		if err := ValidateExtension(extension); err != nil {
			return capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_UNSPECIFIED, err
		}
		return capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY, nil
	}
	definition, ok := PlatformDefinition(key.GetPlatform())
	if !ok {
		return capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_UNSPECIFIED, fmt.Errorf("unknown platform capability %d", key.GetPlatform())
	}
	return definition.LossPolicy, nil
}

func IsWorkloadRequirement(key *capabilityv1.CapabilityKey) bool {
	if key == nil {
		return false
	}
	if key.GetExtension() != nil {
		return ValidateExtension(key.GetExtension()) == nil
	}
	definition, ok := PlatformDefinition(key.GetPlatform())
	return ok && definition.Audience == AudienceWorkloadRequirement
}
