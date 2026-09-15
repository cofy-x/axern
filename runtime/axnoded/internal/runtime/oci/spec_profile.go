package oci

// ExecutionProfile groups the policies used to build a final OCI spec.
type ExecutionProfile struct {
	Baseline OciBaselinePolicy
}

// DefaultExecutionProfile returns the standard Axern OCI execution policy profile.
func DefaultExecutionProfile() ExecutionProfile {
	return ExecutionProfile{
		Baseline: DefaultBaselinePolicy(),
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
	return p
}
