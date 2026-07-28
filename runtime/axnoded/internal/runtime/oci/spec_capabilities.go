package oci

import (
	"strings"

	spec "github.com/opencontainers/runtime-spec/specs-go"
)

// CapabilityPolicy controls capability additions requested through spec annotations.
type CapabilityPolicy struct {
	AnnotationKey  string
	IncludeAmbient bool
}

// DefaultCapabilityPolicy returns Axern's default annotation-driven capability policy.
func DefaultCapabilityPolicy() CapabilityPolicy {
	return CapabilityPolicy{
		AnnotationKey:  linuxCapabilitiesAnnoKey,
		IncludeAmbient: true,
	}
}

func (p CapabilityPolicy) apply(ociSpec *spec.Spec, annotations map[string]string) {
	if ociSpec == nil || ociSpec.Process == nil || len(annotations) == 0 {
		return
	}

	raw := strings.TrimSpace(annotations[p.AnnotationKey])
	if raw == "" {
		return
	}
	if ociSpec.Process.Capabilities == nil {
		ociSpec.Process.Capabilities = &spec.LinuxCapabilities{}
	}

	sets := []*[]string{
		&ociSpec.Process.Capabilities.Bounding,
		&ociSpec.Process.Capabilities.Effective,
		&ociSpec.Process.Capabilities.Inheritable,
		&ociSpec.Process.Capabilities.Permitted,
	}
	if p.IncludeAmbient {
		sets = append(sets, &ociSpec.Process.Capabilities.Ambient)
	}
	for capName := range strings.SplitSeq(raw, ",") {
		normalized := strings.ToUpper(strings.TrimSpace(capName))
		if normalized == "" {
			continue
		}
		for _, target := range sets {
			*target = appendUnique(*target, normalized)
		}
	}
}
