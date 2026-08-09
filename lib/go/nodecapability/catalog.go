// Package nodecapability owns the canonical node capability catalog and the
// pure validation rules shared by axnoded and controld.
package nodecapability

import (
	"fmt"
	"strings"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type Definition struct {
	Key          capabilityv1.PlatformCapability
	Provider     capabilityv1.CapabilityProvider
	Scope        capabilityv1.CapabilityValidityScope
	LossPolicy   capabilityv1.CapabilityLossPolicy
	Dependencies []capabilityv1.PlatformCapability
}

var definitions = map[capabilityv1.PlatformCapability]Definition{
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING:                       platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE:                        platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET:                        platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_DEGRADE),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER:           platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_HOST_CGROUP, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_BOOT, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT:                platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT:               platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_CGROUP_V2_MEMORY_CONTROLLER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER:             platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA:                     platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT:     platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_XFS_PROJECT_QUOTA, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT:    platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_DERIVED, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_REFRESHABLE, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_FAIL_STOP, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER, capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS:                    platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_EROFS_PROBE, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_MOUNT, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST:     platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST:    platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST:  platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNC_SELF_TEST, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
	capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST: platformDefinition(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST, capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_RUNTIME, capabilityv1.CapabilityLossPolicy_CAPABILITY_LOSS_POLICY_ADMISSION_ONLY),
}

func platformDefinition(key capabilityv1.PlatformCapability, provider capabilityv1.CapabilityProvider, scope capabilityv1.CapabilityValidityScope, lossPolicy capabilityv1.CapabilityLossPolicy, dependencies ...capabilityv1.PlatformCapability) Definition {
	return Definition{Key: key, Provider: provider, Scope: scope, LossPolicy: lossPolicy, Dependencies: append([]capabilityv1.PlatformCapability(nil), dependencies...)}
}

func PlatformDefinition(key capabilityv1.PlatformCapability) (Definition, bool) {
	definition, ok := definitions[key]
	definition.Dependencies = append([]capabilityv1.PlatformCapability(nil), definition.Dependencies...)
	return definition, ok
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

func ObservationOwner(key *capabilityv1.CapabilityKey) (capabilityv1.CapabilityProvider, capabilityv1.CapabilityValidityScope, error) {
	if key == nil {
		return 0, 0, fmt.Errorf("capability key is required")
	}
	if extension := key.GetExtension(); extension != nil {
		if err := ValidateExtension(extension); err != nil {
			return 0, 0, err
		}
		return capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG, capabilityv1.CapabilityValidityScope_CAPABILITY_VALIDITY_SCOPE_CONFIG_STATIC, nil
	}
	definition, ok := PlatformDefinition(key.GetPlatform())
	if !ok {
		return 0, 0, fmt.Errorf("unknown platform capability %d", key.GetPlatform())
	}
	return definition.Provider, definition.Scope, nil
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
	return "extension/" + normalized.GetName() + "\x00" + normalized.GetValue(), nil
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
