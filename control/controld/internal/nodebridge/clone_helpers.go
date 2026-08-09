package nodebridge

import (
	"maps"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

func cloneRuntimeExecutionProfile(in *catalogv1.RuntimeExecutionProfile) *catalogv1.RuntimeExecutionProfile {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*catalogv1.RuntimeExecutionProfile)
}

func cloneResolvedProbe(in *servicev1.ServiceProbe) *privatenodev1.ResolvedProbe {
	if in == nil {
		return nil
	}
	out := &privatenodev1.ResolvedProbe{
		SuccessThreshold: in.GetSuccessThreshold(),
		FailureThreshold: in.GetFailureThreshold(),
	}
	if in.GetInitialDelay() != nil {
		out.InitialDelay = proto.Clone(in.GetInitialDelay()).(*durationpb.Duration)
	}
	if in.GetPeriod() != nil {
		out.Period = proto.Clone(in.GetPeriod()).(*durationpb.Duration)
	}
	if in.GetTimeout() != nil {
		out.Timeout = proto.Clone(in.GetTimeout()).(*durationpb.Duration)
	}
	if http := in.GetHttp(); http != nil {
		out.Action = &privatenodev1.ResolvedProbe_Http{
			Http: &privatenodev1.ResolvedHttpProbe{
				Port:   http.GetPort(),
				Path:   http.GetPath(),
				Scheme: privatenodev1.HttpProbeScheme(http.GetScheme()),
			},
		}
	}
	if tcp := in.GetTcp(); tcp != nil {
		out.Action = &privatenodev1.ResolvedProbe_Tcp{
			Tcp: &privatenodev1.ResolvedTcpProbe{Port: tcp.GetPort()},
		}
	}
	return out
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

func cloneCapabilityDependencies(in []*capabilityv1.CapabilityDependency) []*capabilityv1.CapabilityDependency {
	if len(in) == 0 {
		return nil
	}
	out := make([]*capabilityv1.CapabilityDependency, 0, len(in))
	for _, dependency := range in {
		if dependency != nil {
			out = append(out, proto.Clone(dependency).(*capabilityv1.CapabilityDependency))
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
