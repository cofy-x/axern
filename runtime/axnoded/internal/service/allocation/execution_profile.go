package allocation

import (
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
)

func executionProfileFromLanguageRuntime(lrt *langrtmanager.LanguageRuntime) *runtimeoci.ExecutionProfile {
	if lrt == nil {
		return nil
	}
	return ExecutionProfileFromProto(lrt.ExecutionProfile)
}

func ExecutionProfileFromProto(in *catalogv1.RuntimeExecutionProfile) *runtimeoci.ExecutionProfile {
	if in == nil {
		return nil
	}
	out := runtimeoci.DefaultExecutionProfile()
	if baseline := in.GetRuntimeBaseline(); baseline != nil {
		if len(baseline.GetCapabilities()) > 0 {
			out.RuntimeBaseline.Capabilities = append([]string(nil), baseline.GetCapabilities()...)
		}
		if baseline.GetNoFileLimit() > 0 {
			out.RuntimeBaseline.NoFileLimit = baseline.GetNoFileLimit()
		}
	}
	if capability := in.GetCapabilities(); capability != nil {
		if capability.GetAnnotationKey() != "" {
			out.Capabilities.AnnotationKey = capability.GetAnnotationKey()
		}
		if capability.IncludeAmbient != nil {
			out.Capabilities.IncludeAmbient = capability.GetIncludeAmbient()
		}
	}
	if network := in.GetNetworkNamespace(); network != nil {
		if network.GetAnnotationKey() != "" {
			out.NetworkNamespace.AnnotationKey = network.GetAnnotationKey()
		}
	}
	if resources := in.GetResources(); resources != nil && len(resources.GetIgnoreAnnotationKeys()) > 0 {
		out.Resources.IgnoreAnnotationKeys = append([]string(nil), resources.GetIgnoreAnnotationKeys()...)
	}
	return &out
}
