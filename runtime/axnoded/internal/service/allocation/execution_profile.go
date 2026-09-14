package allocation

import (
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
)

func executionProfileFromPreparedEnvironment(lrt *environmentcache.PreparedEnvironment) *runtimeoci.ExecutionProfile {
	if lrt == nil {
		return nil
	}
	return ExecutionProfileFromProto(lrt.ExecutionProfile)
}

func ExecutionProfileFromProto(in *environmentv1.OciExecutionProfile) *runtimeoci.ExecutionProfile {
	if in == nil {
		return nil
	}
	out := runtimeoci.DefaultExecutionProfile()
	if baseline := in.GetBaseline(); baseline != nil {
		if len(baseline.GetCapabilities()) > 0 {
			out.Baseline.Capabilities = append([]string(nil), baseline.GetCapabilities()...)
		}
		if baseline.GetNoFileLimit() > 0 {
			out.Baseline.NoFileLimit = baseline.GetNoFileLimit()
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
