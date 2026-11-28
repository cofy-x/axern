package nodecapability

import (
	"errors"
	"strings"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

type operatorReasonError struct {
	error
	reason string
}

func (e operatorReasonError) Unwrap() error { return e.error }

func withOperatorReason(err error, reason string) error {
	return operatorReasonError{error: err, reason: reason}
}

// RequirementFailureReason returns the bounded operator diagnostic for a
// ResolveRequirements failure. Never render the underlying error to the
// operator: validation errors may include node-provided values.
func RequirementFailureReason(err error) string {
	var failure operatorReasonError
	if errors.As(err, &failure) {
		return failure.reason
	}
	return "node capability requirements could not be resolved"
}

func platformName(key *capabilityv1.CapabilityKey) string {
	platform := key.GetPlatform()
	if _, known := PlatformDefinition(platform); !known {
		return ""
	}
	return strings.TrimPrefix(platform.String(), "PLATFORM_CAPABILITY_")
}

// describeUnavailable runs only after structural observation validation. It
// uses the same dependency availability predicate as admission and includes
// only known platform names and reason codes in the diagnostic.
func describeUnavailable(byKey map[string]*capabilityv1.CapabilityObservation, key *capabilityv1.CapabilityKey, now time.Time) string {
	name := platformName(key)
	if name == "" {
		return "required capability is unavailable"
	}
	id, _ := KeyID(key)
	observation := byKey[id]
	if observation == nil {
		return name + " is missing"
	}
	code := observation.GetReasonCode()
	available := observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
	if code == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED ||
		(available && errors.Is(validateFreshness(key, observation.GetObservedAt(), observation.GetValidUntil(), now, true), errObservationExpired)) {
		return name + " has expired"
	}
	if available || code == capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE {
		dependencies, _ := PlatformDependencyKeys(key.GetPlatform())
		for _, dependency := range dependencies {
			dependencyID, _ := KeyID(dependency)
			if !availableObservationSet(byKey, dependencyID, now, make(map[string]bool)) {
				return name + " has an unavailable dependency. " + describeUnavailable(byKey, dependency, now)
			}
		}
	}
	if validCapabilityReasonCode(code) && code != capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE {
		return name + " is unavailable (" + strings.TrimPrefix(code.String(), "CAPABILITY_REASON_CODE_") + ")"
	}
	return name + " is unavailable"
}
