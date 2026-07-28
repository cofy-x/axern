package placement

import (
	"strconv"
	"strings"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func (p *Selector) buildRequest(env *environmentv1.Environment, config *commonv1.ExecutionConfig) *Request {
	template := env.GetResolvedTemplate()
	requests := config.GetResources().GetRequests()
	return &Request{
		RootfsKey:              firstNonEmpty(env.GetID(), template.GetImageDescriptor().GetDigest()),
		RootfsType:             controlnodev1.RootfsType_ROOTFS_TYPE_IMAGE,
		MountType:              controlnodev1.MountType_MOUNT_TYPE_OCI,
		Runtime:                firstNonEmpty(config.GetRuntimeClass(), p.defaultSandboxRuntime),
		RequestedCpuMilli:      requests.GetCpuMilli(),
		RequestedMemoryBytes:   requests.GetMemoryBytes(),
		Ports:                  portSpecsToPlacementPorts(config.GetPorts()),
		Network:                networkSpecToPlacementNetwork(config.GetNetwork()),
		CapabilityRequirements: capabilityRequirementsToPlacement(config.GetCapabilityRequirements()),
		NodeSelector:           cloneStringMap(config.GetPlacement().GetNodeSelector()),
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

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
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

func capabilityRequirementsToPlacement(in []*commonv1.CapabilityRequirement) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, req := range in {
		if req == nil || strings.TrimSpace(req.GetName()) == "" {
			continue
		}
		if strings.TrimSpace(req.GetValue()) != "" {
			out = append(out, strings.TrimSpace(req.GetName())+"="+strings.TrimSpace(req.GetValue()))
			continue
		}
		out = append(out, strings.TrimSpace(req.GetName()))
	}
	return normalizeStringSlice(out)
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
