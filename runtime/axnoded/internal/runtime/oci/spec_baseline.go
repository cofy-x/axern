package oci

import spec "github.com/opencontainers/runtime-spec/specs-go"

const defaultNoFileLimit uint64 = 1048576

// OciBaselinePolicy defines Axern's managed process defaults for generated OCI specs.
type OciBaselinePolicy struct {
	Capabilities []string
	NoFileLimit  uint64
}

// DefaultBaselinePolicy returns the default managed process baseline.
func DefaultBaselinePolicy() OciBaselinePolicy {
	return OciBaselinePolicy{
		Capabilities: append([]string(nil), defaultLinuxCapabilities...),
		NoFileLimit:  defaultNoFileLimit,
	}
}

func (p OciBaselinePolicy) apply(ociSpec *spec.Spec) {
	if ociSpec == nil || ociSpec.Process == nil {
		return
	}
	p.ensureCapabilities(ociSpec.Process)
	p.ensureNoFileLimit(ociSpec.Process)
}

func (p OciBaselinePolicy) ensureCapabilities(process *spec.Process) {
	if process.Capabilities == nil {
		process.Capabilities = &spec.LinuxCapabilities{}
	}
	sets := []*[]string{
		&process.Capabilities.Bounding,
		&process.Capabilities.Effective,
		&process.Capabilities.Inheritable,
		&process.Capabilities.Permitted,
	}
	for _, capName := range p.Capabilities {
		for _, target := range sets {
			*target = appendUnique(*target, capName)
		}
	}
}

func (p OciBaselinePolicy) ensureNoFileLimit(process *spec.Process) {
	if p.NoFileLimit == 0 {
		return
	}
	for idx := range process.Rlimits {
		if process.Rlimits[idx].Type != "RLIMIT_NOFILE" {
			continue
		}
		if process.Rlimits[idx].Soft < p.NoFileLimit {
			process.Rlimits[idx].Soft = p.NoFileLimit
		}
		if process.Rlimits[idx].Hard < p.NoFileLimit {
			process.Rlimits[idx].Hard = p.NoFileLimit
		}
		return
	}
	process.Rlimits = append(process.Rlimits, spec.POSIXRlimit{
		Type: "RLIMIT_NOFILE",
		Hard: p.NoFileLimit,
		Soft: p.NoFileLimit,
	})
}
