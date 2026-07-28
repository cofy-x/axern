package oci

// ExecutionProfile groups the policies used to build a final OCI spec.
type ExecutionProfile struct {
	RuntimeBaseline  RuntimeBaselinePolicy
	Capabilities     CapabilityPolicy
	NetworkNamespace NetworkNamespacePolicy
	Resources        ResourcePolicy
}

// DefaultExecutionProfile returns the standard Axern OCI execution policy profile.
func DefaultExecutionProfile() ExecutionProfile {
	return ExecutionProfile{
		RuntimeBaseline:  DefaultRuntimeBaselinePolicy(),
		Capabilities:     DefaultCapabilityPolicy(),
		NetworkNamespace: DefaultNetworkNamespacePolicy(),
		Resources:        DefaultResourcePolicy(),
	}
}

func (p ExecutionProfile) withDefaults() ExecutionProfile {
	defaults := DefaultExecutionProfile()
	if p.RuntimeBaseline.Capabilities == nil {
		p.RuntimeBaseline.Capabilities = defaults.RuntimeBaseline.Capabilities
	}
	if p.RuntimeBaseline.NoFileLimit == 0 {
		p.RuntimeBaseline.NoFileLimit = defaults.RuntimeBaseline.NoFileLimit
	}
	if p.Capabilities.AnnotationKey == "" {
		p.Capabilities = defaults.Capabilities
	}
	if p.NetworkNamespace.AnnotationKey == "" {
		p.NetworkNamespace.AnnotationKey = defaults.NetworkNamespace.AnnotationKey
	}
	if p.Resources.IgnoreAnnotationKeys == nil {
		p.Resources.IgnoreAnnotationKeys = defaults.Resources.IgnoreAnnotationKeys
	}
	return p
}
