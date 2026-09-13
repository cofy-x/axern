package oci

// ExecutionProfile groups the policies used to build a final OCI spec.
type ExecutionProfile struct {
	Baseline         OciBaselinePolicy
	NetworkNamespace NetworkNamespacePolicy
	Resources        ResourcePolicy
}

// DefaultExecutionProfile returns the standard Axern OCI execution policy profile.
func DefaultExecutionProfile() ExecutionProfile {
	return ExecutionProfile{
		Baseline:         DefaultBaselinePolicy(),
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
	if p.NetworkNamespace.AnnotationKey == "" {
		p.NetworkNamespace.AnnotationKey = defaults.NetworkNamespace.AnnotationKey
	}
	if p.Resources.IgnoreAnnotationKeys == nil {
		p.Resources.IgnoreAnnotationKeys = defaults.Resources.IgnoreAnnotationKeys
	}
	return p
}
