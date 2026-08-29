package nodecapability

import (
	"fmt"
	"sort"
	"strings"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

// RequirementInput is the runtime-neutral input used by both controld and
// axnoded. Facts unavailable before image preparation (for example EROFS) are
// left false during the request-static gate and supplied at the backing gate.
type RequirementInput struct {
	RuntimeName                     string
	HasPorts                        bool
	NetworkMode                     string
	NetworkBackend                  string
	RequiresDNSPolicyEnforcement    bool
	RequiresStrictEgressEnforcement bool
	MemoryLimitBytes                int64
	RootfsWritable                  bool
	EphemeralStorageLimitBytes      int64
	EROFSBacking                    bool
	ExtensionCapabilityRequests     []*capabilityv1.ExtensionCapabilityRequirement
}

func DeriveRequirements(input RequirementInput) ([]*capabilityv1.CapabilityKey, error) {
	return deriveRequirements(input, false)
}

// DeriveRequestStaticRequirements derives the requirement subset that is
// independent of a selected node. Controld uses it before a node snapshot is
// available and for rejection diagnostics when that node has no operational
// network backend. Durable admission must use DeriveRequirements instead.
func DeriveRequestStaticRequirements(input RequirementInput) ([]*capabilityv1.CapabilityKey, error) {
	return deriveRequirements(input, true)
}

func deriveRequirements(input RequirementInput, deferNetworkBackend bool) ([]*capabilityv1.CapabilityKey, error) {
	if input.MemoryLimitBytes < 0 || input.EphemeralStorageLimitBytes < 0 {
		return nil, fmt.Errorf("resource limits cannot be negative")
	}
	if err := ValidateExtensionRequirements(input.ExtensionCapabilityRequests); err != nil {
		return nil, err
	}
	runtimeName := strings.ToLower(strings.TrimSpace(input.RuntimeName))
	if runtimeName != "runc" && runtimeName != "runsc" {
		return nil, fmt.Errorf("unsupported sandbox runtime %q", input.RuntimeName)
	}
	keys := make([]*capabilityv1.CapabilityKey, 0, len(input.ExtensionCapabilityRequests)+5)
	if input.HasPorts {
		keys = append(keys, PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING))
	}
	if input.RequiresDNSPolicyEnforcement && input.RequiresStrictEgressEnforcement {
		return nil, fmt.Errorf("network policy cannot require both DNS-only and strict enforcement")
	}
	if input.RequiresDNSPolicyEnforcement {
		keys = append(keys, PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_DNS_POLICY_ENFORCEMENT))
	}
	if input.RequiresStrictEgressEnforcement {
		keys = append(keys, PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_STRICT_EGRESS_ENFORCEMENT))
	}
	mode := strings.ToLower(strings.TrimSpace(input.NetworkMode))
	if mode != "host" {
		backend := strings.ToLower(strings.TrimSpace(input.NetworkBackend))
		if backend == "" && !deferNetworkBackend {
			return nil, fmt.Errorf("non-host network requires an observed network backend")
		}
		switch backend {
		case "":
		case "bridge", "iptables":
			keys = append(keys, PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE))
		case "bpfnet", "ebpf":
			keys = append(keys, PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET))
		default:
			return nil, fmt.Errorf("unsupported network backend %q", input.NetworkBackend)
		}
	}
	if input.MemoryLimitBytes > 0 {
		capability := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT
		if runtimeName == "runsc" {
			capability = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT
		}
		keys = append(keys, PlatformKey(capability))
	}
	if input.RootfsWritable || input.EphemeralStorageLimitBytes > 0 {
		capability := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT
		if runtimeName == "runsc" {
			capability = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT
		}
		keys = append(keys, PlatformKey(capability))
	}
	if input.EROFSBacking {
		keys = append(keys, PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_ROOTFS_LOWER_EROFS))
	}
	for _, requirement := range input.ExtensionCapabilityRequests {
		capability := requirement.GetCapability()
		keys = append(keys, ExtensionKey(capability.GetName(), capability.GetValue()))
	}
	sort.Slice(keys, func(i, j int) bool {
		left, _ := KeyID(keys[i])
		right, _ := KeyID(keys[j])
		return left < right
	})
	if err := ValidateRequirementKeys(keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// AvailableNetworkBackend returns the concrete backend represented by the
// current node snapshot. Backend selection is kept beside requirement
// derivation so controld and repository verification clients cannot drift.
func AvailableNetworkBackend(snapshot *capabilityv1.CapabilitySnapshot, now time.Time) string {
	for _, candidate := range []struct {
		platform capabilityv1.PlatformCapability
		backend  string
	}{
		{platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET, backend: "bpfnet"},
		{platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BRIDGE, backend: "bridge"},
	} {
		if _, available := AvailableObservation(snapshot, PlatformKey(candidate.platform), now); available {
			return candidate.backend
		}
	}
	return ""
}

func RequirementKeysEqual(left, right []*capabilityv1.CapabilityKey) bool {
	if len(left) != len(right) {
		return false
	}
	leftIDs := make([]string, 0, len(left))
	rightIDs := make([]string, 0, len(right))
	for _, key := range left {
		id, err := KeyID(key)
		if err != nil {
			return false
		}
		leftIDs = append(leftIDs, id)
	}
	for _, key := range right {
		id, err := KeyID(key)
		if err != nil {
			return false
		}
		rightIDs = append(rightIDs, id)
	}
	sort.Strings(leftIDs)
	sort.Strings(rightIDs)
	for index := range leftIDs {
		if leftIDs[index] != rightIDs[index] {
			return false
		}
	}
	return true
}
