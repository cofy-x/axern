package nodecapability

import (
	"fmt"
	"regexp"
	"strings"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	namePattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[-_.A-Za-z0-9]*[A-Za-z0-9])?$`)
)

var reservedExtensionDomains = map[string]struct{}{
	"axern.io":  {},
	"axern.dev": {},
}

func ValidateExtension(extension *capabilityv1.ExtensionCapability) error {
	if extension == nil {
		return fmt.Errorf("extension capability is required")
	}
	normalized := NormalizeExtension(extension)
	name := normalized.GetName()
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("extension capability name %q must be <dns-domain>/<name>", name)
	}
	domain := strings.ToLower(parts[0])
	if _, reserved := reservedExtensionDomains[domain]; reserved {
		return fmt.Errorf("extension capability domain %q is reserved", domain)
	}
	if len(domain) > 253 {
		return fmt.Errorf("extension capability domain is too long")
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || !dnsLabelPattern.MatchString(label) {
			return fmt.Errorf("extension capability domain %q is invalid", domain)
		}
	}
	if len(parts[1]) > 63 || !namePattern.MatchString(parts[1]) {
		return fmt.Errorf("extension capability name segment %q is invalid", parts[1])
	}
	if strings.ContainsRune(normalized.GetValue(), '\x00') {
		return fmt.Errorf("extension capability value contains NUL")
	}
	return nil
}

// NormalizeExtension canonicalizes the DNS portion of a qualified name while
// preserving the value byte-for-byte. Values have exact-match semantics and
// must never be silently trimmed or case-folded.
func NormalizeExtension(extension *capabilityv1.ExtensionCapability) *capabilityv1.ExtensionCapability {
	if extension == nil {
		return nil
	}
	name := strings.TrimSpace(extension.GetName())
	if domain, suffix, ok := strings.Cut(name, "/"); ok {
		name = strings.ToLower(domain) + "/" + suffix
	}
	return &capabilityv1.ExtensionCapability{Name: name, Value: extension.GetValue()}
}

func ValidateExtensionRequirement(requirement *capabilityv1.ExtensionCapabilityRequirement) error {
	if requirement == nil {
		return fmt.Errorf("extension capability requirement is required")
	}
	return ValidateExtension(requirement.GetCapability())
}
