package placement

import (
	"strconv"
	"strings"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func (p *Selector) buildRequest(env *environmentv1.Environment, config *commonv1.ExecutionConfig) *placementkernel.Request {
	template := env.GetResolvedTemplate()
	requests := config.GetResources().GetRequests()
	runtimeName := firstNonEmpty(config.GetRuntimeClass(), p.defaultSandboxRuntime)
	capabilities := extensionRequirementsToPlacement(config.GetExtensionCapabilityRequirements())
	ports := portSpecsToPlacementPorts(config.GetPorts())
	if len(ports) > 0 {
		capabilities = appendCapabilityKey(capabilities, capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING))
	}
	network := networkSpecToPlacementNetwork(config.GetNetwork())
	if !template.GetRootfsReadonly() || requests.GetEphemeralStorageBytes() > 0 {
		platform := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT
		if runtimeName == "runsc" {
			platform = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT
		}
		capabilities = appendCapabilityKey(capabilities, capabilitycontract.PlatformKey(platform))
	}
	if config.GetResources().GetLimits().GetMemoryBytes() > 0 {
		platform := capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT
		if runtimeName == "runsc" {
			platform = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT
		}
		capabilities = appendCapabilityKey(capabilities, capabilitycontract.PlatformKey(platform))
	}
	return &placementkernel.Request{
		RootfsKey:                      firstNonEmpty(env.GetID(), template.GetImageDescriptor().GetDigest()),
		RootfsType:                     controlnodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:                      controlnodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:                        runtimeName,
		RequestedCpuMilli:              requests.GetCpuMilli(),
		RequestedMemoryBytes:           requests.GetMemoryBytes(),
		RequestedEphemeralStorageBytes: requests.GetEphemeralStorageBytes(),
		Ports:                          ports,
		Network:                        network,
		CapabilityRequirements:         capabilities,
		NodeSelector:                   cloneStringMap(config.GetPlacement().GetNodeSelector()),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func portSpecsToPlacementPorts(in []*commonv1.PortSpec) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, port := range in {
		if port == nil {
			continue
		}
		protocol := strings.ToLower(strings.TrimPrefix(port.GetProtocol().String(), "PORT_PROTOCOL_"))
		if protocol == "" || protocol == "unspecified" {
			protocol = "tcp"
		}
		if port.GetHostPort() > 0 {
			out = append(out, protocol+":"+strconv.Itoa(int(port.GetHostPort()))+":"+strconv.Itoa(int(port.GetContainerPort())))
			continue
		}
		if port.GetContainerPort() > 0 {
			out = append(out, protocol+":"+strconv.Itoa(int(port.GetContainerPort())))
		}
	}
	return out
}

func networkSpecToPlacementNetwork(in *commonv1.NetworkSpec) string {
	if in == nil || in.GetMode() == commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(in.GetMode().String(), "NETWORK_MODE_"))
}

func extensionRequirementsToPlacement(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.CapabilityKey {
	if len(in) == 0 {
		return nil
	}
	out := make([]*capabilityv1.CapabilityKey, 0, len(in))
	for _, req := range in {
		if req == nil || req.GetCapability() == nil {
			continue
		}
		out = appendCapabilityKey(out, capabilitycontract.ExtensionKey(req.GetCapability().GetName(), req.GetCapability().GetValue()))
	}
	return out
}

func appendCapabilityKey(in []*capabilityv1.CapabilityKey, key *capabilityv1.CapabilityKey) []*capabilityv1.CapabilityKey {
	want, err := capabilitycontract.KeyID(key)
	if err != nil {
		return in
	}
	for _, existing := range in {
		id, _ := capabilitycontract.KeyID(existing)
		if id == want {
			return in
		}
	}
	return append(in, key)
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
