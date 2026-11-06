package oci

import (
	"fmt"
	"slices"
)

// ExecutionProfile groups the policies used to build a final OCI spec.
type ExecutionProfile struct {
	Baseline OciBaselinePolicy

	// Task limits are a paired node policy. Both zero preserves the baseline.
	GuestTaskLimit int64
	HostTaskLimit  int64
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
	p.Baseline.Capabilities = slices.Clone(p.Baseline.Capabilities)
	if p.Baseline.NoFileLimit == 0 {
		p.Baseline.NoFileLimit = defaults.Baseline.NoFileLimit
	}
	return p
}

func (p ExecutionProfile) validate() error {
	if p.GuestTaskLimit == 0 && p.HostTaskLimit == 0 {
		return nil
	}
	if p.GuestTaskLimit < 64 || p.GuestTaskLimit > 65536 || p.HostTaskLimit < 64 || p.HostTaskLimit > 65536 {
		return fmt.Errorf("guest and host task limits must both be zero or both be between 64 and 65536")
	}
	return nil
}

func (p ExecutionProfile) equal(other ExecutionProfile) bool {
	return p.GuestTaskLimit == other.GuestTaskLimit && p.HostTaskLimit == other.HostTaskLimit &&
		p.Baseline.NoFileLimit == other.Baseline.NoFileLimit &&
		slices.Equal(p.Baseline.Capabilities, other.Baseline.Capabilities)
}
