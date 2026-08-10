package parse

import (
	"fmt"
	"strings"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func ExtensionCapabilities(values []string) ([]*capabilityv1.ExtensionCapabilityRequirement, error) {
	result := make([]*capabilityv1.ExtensionCapabilityRequirement, 0, len(values))
	for _, raw := range values {
		name, value, _ := strings.Cut(raw, "=")
		if name == "" || name != strings.TrimSpace(name) {
			return nil, fmt.Errorf("invalid extension capability %q, want <dns-domain>/<name>[=value]", raw)
		}
		result = append(result, &capabilityv1.ExtensionCapabilityRequirement{Capability: &capabilityv1.ExtensionCapability{Name: name, Value: value}})
	}
	if err := capabilitycontract.ValidateExtensionRequirements(result); err != nil {
		return nil, err
	}
	return result, nil
}
