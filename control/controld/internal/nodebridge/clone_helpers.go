package nodebridge

import (
	"maps"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/protobuf/proto"
)

func cloneOciExecutionProfile(in *catalogv1.OciExecutionProfile) *catalogv1.OciExecutionProfile {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*catalogv1.OciExecutionProfile)
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

func mergeStringMaps(base, override map[string]string) map[string]string {
	out := cloneStringMap(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range override {
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func clonePortSpecs(in []*commonv1.PortSpec) []*commonv1.PortSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]*commonv1.PortSpec, 0, len(in))
	for _, port := range in {
		if port == nil {
			continue
		}
		out = append(out, proto.Clone(port).(*commonv1.PortSpec))
	}
	return out
}

func cloneNetworkSpec(in *commonv1.NetworkSpec) *commonv1.NetworkSpec {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*commonv1.NetworkSpec)
}

func cloneExtensionCapabilityRequirements(in []*capabilityv1.ExtensionCapabilityRequirement) []*capabilityv1.ExtensionCapabilityRequirement {
	if len(in) == 0 {
		return nil
	}
	out := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(in))
	for _, req := range in {
		if req == nil {
			continue
		}
		out = append(out, proto.Clone(req).(*capabilityv1.ExtensionCapabilityRequirement))
	}
	return out
}

func cloneCapabilityRequirements(in []*capabilityv1.CapabilityRequirement) []*capabilityv1.CapabilityRequirement {
	if len(in) == 0 {
		return nil
	}
	out := make([]*capabilityv1.CapabilityRequirement, 0, len(in))
	for _, dependency := range in {
		if dependency != nil {
			out = append(out, proto.Clone(dependency).(*capabilityv1.CapabilityRequirement))
		}
	}
	return out
}

func cloneCapabilityConditions(in []*capabilityv1.CapabilityCondition) []*capabilityv1.CapabilityCondition {
	out := make([]*capabilityv1.CapabilityCondition, 0, len(in))
	for _, condition := range in {
		if condition != nil {
			out = append(out, proto.Clone(condition).(*capabilityv1.CapabilityCondition))
		}
	}
	return out
}

func cloneCapabilityConditionSet(in *capabilityv1.CapabilityConditionSet) *capabilityv1.CapabilityConditionSet {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*capabilityv1.CapabilityConditionSet)
}

func cloneResolvedSecretEnvVars(in []*privatenodev1.ResolvedSecretEnvVar) []*privatenodev1.ResolvedSecretEnvVar {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatenodev1.ResolvedSecretEnvVar, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, &privatenodev1.ResolvedSecretEnvVar{Name: item.GetName(), Value: item.GetValue()})
	}
	return out
}

func cloneResolvedSecretFiles(in []*privatenodev1.ResolvedSecretFile) []*privatenodev1.ResolvedSecretFile {
	if len(in) == 0 {
		return nil
	}
	out := make([]*privatenodev1.ResolvedSecretFile, 0, len(in))
	for _, item := range in {
		if item == nil {
			continue
		}
		out = append(out, &privatenodev1.ResolvedSecretFile{
			Path:    item.GetPath(),
			Content: append([]byte(nil), item.GetContent()...),
			Mode:    item.GetMode(),
		})
	}
	return out
}
