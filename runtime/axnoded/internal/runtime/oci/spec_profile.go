package oci

// ExecutionProfile groups the policies used to build a final OCI spec.
type ExecutionProfile struct {
	Baseline         OciBaselinePolicy
	Capabilities     CapabilityPolicy
	NetworkNamespace NetworkNamespacePolicy
	Resources        ResourcePolicy
}

// DefaultExecutionProfile returns the standard Axern OCI execution policy profile.
func DefaultExecutionProfile() ExecutionProfile {
	return ExecutionProfile{
		Baseline:         DefaultBaselinePolicy(),
		Capabilities:     DefaultCapabilityPolicy(),
		NetworkNamespace: DefaultNetworkNamespacePolicy(),
		Resources:        DefaultResourcePolicy(),
	}
}

func (p ExecutionProfile) withDefaults() ExecutionProfile {
	defaults := DefaultExecutionProfile()
	if p.Baseline.Capabilities == nil {
		p.Baseline.Capabilities = defaults.Baseline.Capabilities
	}
	if p.Baseline.NoFileLimit == 0 {
		p.Baseline.NoFileLimit = defaults.Baseline.NoFileLimit
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
