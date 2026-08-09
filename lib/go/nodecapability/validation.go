package nodecapability

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

var (
	dnsLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)
	namePattern     = regexp.MustCompile(`^[A-Za-z0-9](?:[-_.A-Za-z0-9]*[A-Za-z0-9])?$`)
)

var reservedExtensionDomains = map[string]struct{}{
	"axern.io":  {},
	"axern.dev": {},
}

const (
	MaxExtensionCapabilities = 64
	MaxExtensionValueBytes   = 256
	MaxReasonBytes           = 1024
	MaxObservationProofs     = 32
	MaxSnapshotObservations  = 128
	MaxIdentityBytes         = 512
)

func validCapabilityState(value capabilityv1.CapabilityState) bool {
	_, ok := capabilityv1.CapabilityState_name[int32(value)]
	return ok && value != capabilityv1.CapabilityState_CAPABILITY_STATE_UNSPECIFIED
}

func validCapabilityReasonCode(value capabilityv1.CapabilityReasonCode) bool {
	_, ok := capabilityv1.CapabilityReasonCode_name[int32(value)]
	return ok && value != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_UNSPECIFIED
}

func validCapabilityConditionState(value capabilityv1.CapabilityConditionState) bool {
	_, ok := capabilityv1.CapabilityConditionState_name[int32(value)]
	return ok && value != capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_UNSPECIFIED
}

func validateBoundedIdentity(name, value string) error {
	if !utf8.ValidString(value) || len(value) == 0 || len(value) > MaxIdentityBytes || !boundedIdentityPattern.MatchString(value) {
		return fmt.Errorf("%s has invalid format or size", name)
	}
	return nil
}

func ValidateExtension(extension *capabilityv1.ExtensionCapability) error {
	if extension == nil {
		return fmt.Errorf("extension capability is required")
	}
	if extension.GetName() != strings.TrimSpace(extension.GetName()) {
		return fmt.Errorf("extension capability name %q must not contain surrounding whitespace", extension.GetName())
	}
	normalized := NormalizeExtension(extension)
	name := normalized.GetName()
	parts := strings.Split(name, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("extension capability name %q must be <dns-domain>/<name>", name)
	}
	domain := strings.ToLower(parts[0])
	for reserved := range reservedExtensionDomains {
		if domain == reserved || strings.HasSuffix(domain, "."+reserved) {
			return fmt.Errorf("extension capability domain %q is reserved", domain)
		}
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
	if !utf8.ValidString(normalized.GetValue()) {
		return fmt.Errorf("extension capability value is not valid UTF-8")
	}
	if strings.ContainsRune(normalized.GetValue(), '\x00') {
		return fmt.Errorf("extension capability value contains NUL")
	}
	if len(normalized.GetValue()) > MaxExtensionValueBytes {
		return fmt.Errorf("extension capability value exceeds %d bytes", MaxExtensionValueBytes)
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
	name := extension.GetName()
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

func ValidateExtensionRequirements(requirements []*capabilityv1.ExtensionCapabilityRequirement) error {
	if len(requirements) > MaxExtensionCapabilities {
		return fmt.Errorf("extension capability count exceeds %d", MaxExtensionCapabilities)
	}
	seen := make(map[string]struct{}, len(requirements))
	for index, requirement := range requirements {
		if err := ValidateExtensionRequirement(requirement); err != nil {
			return fmt.Errorf("extension capability %d: %w", index, err)
		}
		name := NormalizeExtension(requirement.GetCapability()).GetName()
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate extension capability %q", requirement.GetCapability().GetName())
		}
		seen[name] = struct{}{}
	}
	return nil
}

// BoundedReason preserves the operator-facing diagnostic while preventing
// provider-controlled payloads from growing snapshots, rows, or log records
// without bound. ReasonCode remains the stable machine-readable diagnosis.
func BoundedReason(reason string) string {
	reason = strings.ToValidUTF8(reason, "\uFFFD")
	if len(reason) <= MaxReasonBytes {
		return reason
	}
	end := MaxReasonBytes
	for end > 0 && !utf8.RuneStart(reason[end]) {
		end--
	}
	return reason[:end]
}

// ValidateConditionSet validates the full allocation-owned projection. A
// degraded or failed condition may retain an expired proof for diagnosis; a
// healthy condition must prove that its observation was current when the
// condition was observed.
func ValidateConditionSet(set *capabilityv1.CapabilityConditionSet, now time.Time) error {
	if set == nil || set.GetRevision() <= 0 || set.GetObservedAt() == nil {
		return fmt.Errorf("capability condition set, positive revision, and observed_at are required")
	}
	if err := set.GetObservedAt().CheckValid(); err != nil {
		return fmt.Errorf("capability condition set observed_at: %w", err)
	}
	setObservedAt := set.GetObservedAt().AsTime()
	if setObservedAt.After(now.Add(time.Minute)) {
		return fmt.Errorf("capability condition set observed_at is in the future")
	}
	if len(set.GetConditions()) > MaxExtensionCapabilities+16 {
		return fmt.Errorf("capability condition count is too large")
	}
	seen := make(map[string]struct{}, len(set.GetConditions()))
	for _, condition := range set.GetConditions() {
		if condition == nil || condition.GetObservedAt() == nil {
			return fmt.Errorf("capability condition and observed_at are required")
		}
		id, err := KeyID(condition.GetKey())
		if err != nil {
			return err
		}
		if err := condition.GetObservedAt().CheckValid(); err != nil {
			return fmt.Errorf("capability condition %q observed_at: %w", id, err)
		}
		if !IsWorkloadRequirement(condition.GetKey()) {
			return fmt.Errorf("internal capability %q cannot be an allocation condition", id)
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("duplicate capability condition %q", id)
		}
		seen[id] = struct{}{}
		if !validCapabilityConditionState(condition.GetState()) || !validCapabilityReasonCode(condition.GetReasonCode()) {
			return fmt.Errorf("capability condition %q has an invalid state or reason code", id)
		}
		if condition.GetState() == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY &&
			condition.GetReasonCode() != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
			return fmt.Errorf("healthy capability condition %q must use available reason code", id)
		}
		if condition.GetState() != capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY &&
			condition.GetReasonCode() == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
			return fmt.Errorf("non-healthy capability condition %q cannot use available reason code", id)
		}
		if !utf8.ValidString(condition.GetMessage()) || len(condition.GetMessage()) > MaxReasonBytes {
			return fmt.Errorf("capability condition %q message exceeds %d bytes", id, MaxReasonBytes)
		}
		conditionAt := condition.GetObservedAt().AsTime()
		if conditionAt.After(setObservedAt) {
			return fmt.Errorf("capability condition %q was observed after its condition set was published", id)
		}
		if conditionAt.After(now.Add(time.Minute)) {
			return fmt.Errorf("capability condition %q observed_at is in the future", id)
		}
		proof := condition.GetProof()
		if proof == nil {
			if condition.GetState() == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY {
				return fmt.Errorf("healthy capability condition %q is missing proof", id)
			}
			continue
		}
		if !proto.Equal(condition.GetKey(), proof.GetKey()) {
			return fmt.Errorf("capability condition %q proof key mismatch", id)
		}
		if proof.GetObservedAt() != nil && proof.GetObservedAt().AsTime().After(conditionAt) {
			return fmt.Errorf("capability condition %q proof was observed after the condition", id)
		}
		requireCurrent := condition.GetState() == capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY
		if err := validateObservationProof(proof, conditionAt, requireCurrent); err != nil {
			return fmt.Errorf("capability condition %q proof: %w", id, err)
		}
	}
	return nil
}
