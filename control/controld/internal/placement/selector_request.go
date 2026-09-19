package placement

import (
	"fmt"
	"strings"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	controlnodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	networkpolicy "github.com/cofy-x/axern/lib/go/networkpolicy"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func (p *Selector) buildRequest(env *environmentv1.Environment, config *commonv1.ExecutionConfig) (*placementkernel.Request, error) {
	template := env.GetResolvedSpec()
	requests := config.GetResources().GetRequests()
	limits := config.GetResources().GetLimits()
	normalizedNetwork, err := networkpolicy.Normalize(config.GetNetwork())
	if err != nil {
		return nil, fmt.Errorf("normalize placement network policy: %w", err)
	}
	network := networkSpecToPlacementNetwork(normalizedNetwork)
	policyMode := networkpolicy.Mode(normalizedNetwork)
	capabilities, err := capabilitycontract.DeriveRequestStaticRequirements(capabilitycontract.RequirementInput{
		NetworkMode:                     network,
		RequiresDNSPolicyEnforcement:    policyMode == networkpolicy.EnforcementDNSDeny,
		RequiresStrictEgressEnforcement: policyMode == networkpolicy.EnforcementStrict && networkpolicy.StrictNeedsEgressd(normalizedNetwork),
		MemoryLimitBytes:                limits.GetMemoryBytes(),
		RootfsWritable:                  !template.GetRootfsReadonly(),
		EphemeralStorageLimitBytes:      limits.GetEphemeralStorageBytes(),
		RootfsSnapshot:                  config.GetRootfsSnapshot() != nil,
		ExtensionCapabilityRequests:     config.GetExtensionCapabilityRequirements(),
	})
	if err != nil {
		return nil, fmt.Errorf("derive placement capability requirements: %w", err)
	}
	return &placementkernel.Request{
		RootfsKey:                      firstNonEmpty(env.GetID(), template.GetImageDescriptor().GetDigest()),
		RootfsType:                     controlnodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:                      controlnodev1.MountType_MOUNT_TYPE_OCI,
		MemoryLimitBytes:               limits.GetMemoryBytes(),
		RootfsWritable:                 !template.GetRootfsReadonly(),
		EphemeralStorageLimitBytes:     limits.GetEphemeralStorageBytes(),
		RequestedCpuMilli:              requests.GetCpuMilli(),
		RequestedMemoryBytes:           requests.GetMemoryBytes(),
		RequestedEphemeralStorageBytes: requests.GetEphemeralStorageBytes(),
		RootfsSnapshot:                 config.GetRootfsSnapshot() != nil,
		Network:                        network,
		CapabilityRequirements:         capabilities,
		ExtensionCapabilityRequirements: cloneExtensionRequirements(
			config.GetExtensionCapabilityRequirements(),
		),
		NodeSelector: cloneStringMap(config.GetPlacement().GetNodeSelector()),
	}, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func networkSpecToPlacementNetwork(in *commonv1.NetworkSpec) string {
	if networkpolicy.IsStrictDenyAll(in) {
		return "isolated"
	}
	if in == nil || in.GetMode() == commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(in.GetMode().String(), "NETWORK_MODE_"))
}

func cloneExtensionRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.ExtensionCapabilityRequirement {
	if len(in) == 0 {
		return nil
	}
	out := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(in))
	for _, req := range in {
		if req == nil || req.GetCapability() == nil {
			out = append(out, req)
			continue
		}
		out = append(out, &capabilityv1.ExtensionCapabilityRequirement{
			Capability: capabilitycontract.NormalizeExtension(req.GetCapability()),
		})
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
