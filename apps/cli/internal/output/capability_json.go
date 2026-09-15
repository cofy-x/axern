package output

import (
	"strings"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type CapabilityKeyJSON struct {
	Platform  string                   `json:"platform,omitempty"`
	Extension *ExtensionCapabilityJSON `json:"extension,omitempty"`
}

type ExtensionCapabilityJSON struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type CapabilityEvidenceJSON struct {
	IdentityType        string `json:"identity_type"`
	BootID              string `json:"boot_id,omitempty"`
	MountIdentity       string `json:"mount_identity,omitempty"`
	RuntimeBinaryDigest string `json:"runtime_binary_digest,omitempty"`
	RuntimeConfigDigest string `json:"runtime_config_digest,omitempty"`
}

type CapabilityConditionJSON struct {
	Key        *CapabilityKeyJSON `json:"key,omitempty"`
	State      string             `json:"state"`
	ReasonCode string             `json:"reason_code,omitempty"`
	Message    string             `json:"message,omitempty"`
}

type CapabilityConditionSetJSON struct {
	ObservedAt string                     `json:"observed_at,omitempty"`
	Conditions []*CapabilityConditionJSON `json:"conditions"`
}

func newCapabilityConditionSetJSON(set *capabilityv1.CapabilityConditionSet) *CapabilityConditionSetJSON {
	if set == nil {
		return nil
	}
	return &CapabilityConditionSetJSON{
		ObservedAt: FormatProtoTimestamp(set.GetObservedAt()),
		Conditions: newCapabilityConditionJSONs(set.GetConditions()),
	}
}

func newCapabilityConditionJSONs(conditions []*capabilityv1.CapabilityCondition) []*CapabilityConditionJSON {
	if len(conditions) == 0 {
		return nil
	}
	out := make([]*CapabilityConditionJSON, 0, len(conditions))
	for _, condition := range conditions {
		if condition == nil {
			continue
		}
		out = append(out, &CapabilityConditionJSON{
			Key:        newCapabilityKeyJSON(condition.GetKey()),
			State:      capabilityEnumLabel(condition.GetState().String(), "CAPABILITY_CONDITION_STATE_"),
			ReasonCode: capabilityEnumLabel(condition.GetReasonCode().String(), "CAPABILITY_REASON_CODE_"),
			Message:    condition.GetMessage(),
		})
	}
	return out
}

func newCapabilityKeyJSON(key *capabilityv1.CapabilityKey) *CapabilityKeyJSON {
	if key == nil {
		return nil
	}
	switch kind := key.GetKind().(type) {
	case *capabilityv1.CapabilityKey_Platform:
		return &CapabilityKeyJSON{Platform: capabilityEnumLabel(kind.Platform.String(), "PLATFORM_CAPABILITY_")}
	case *capabilityv1.CapabilityKey_Extension:
		if kind.Extension == nil {
			return nil
		}
		return &CapabilityKeyJSON{Extension: &ExtensionCapabilityJSON{Name: kind.Extension.GetName(), Value: kind.Extension.GetValue()}}
	default:
		return nil
	}
}

func newCapabilityEvidenceJSON(evidence *capabilityv1.CapabilityEvidence) *CapabilityEvidenceJSON {
	if evidence == nil {
		return nil
	}
	out := &CapabilityEvidenceJSON{}
	switch identity := evidence.GetIdentity().(type) {
	case *capabilityv1.CapabilityEvidence_Boot:
		out.IdentityType = "boot"
		out.BootID = identity.Boot.GetBootID()
	case *capabilityv1.CapabilityEvidence_Mount:
		out.IdentityType = "mount"
		out.BootID = identity.Mount.GetBootID()
		out.MountIdentity = identity.Mount.GetMountIdentity()
	case *capabilityv1.CapabilityEvidence_Runtime:
		out.IdentityType = "runtime"
		out.BootID = identity.Runtime.GetBootID()
		out.RuntimeBinaryDigest = identity.Runtime.GetRuntimeBinaryDigest()
		out.RuntimeConfigDigest = identity.Runtime.GetRuntimeConfigDigest()
	}
	return out
}

func capabilityEnumLabel(value, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(value, prefix))
}
