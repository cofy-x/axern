package nodecapability

import (
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

const missingObservationReason = "capability observation is absent from the current snapshot"

// EvaluatedObservation is the effective state of one observation at a point
// in time. It deliberately normalizes expiry and absence so every consumer
// applies the same fail-closed transition semantics.
type EvaluatedObservation struct {
	State      capabilityv1.CapabilityState
	ReasonCode capabilityv1.CapabilityReasonCode
	Reason     string
}

// EvaluateObservation applies snapshot dependency and freshness validation to
// an observation. A previously AVAILABLE observation becomes UNKNOWN once it
// expires; an absent observation is also UNKNOWN with a bounded reason code.
func EvaluateObservation(snapshot *capabilityv1.CapabilitySnapshot, observation *capabilityv1.CapabilityObservation, now time.Time) EvaluatedObservation {
	if observation == nil {
		return EvaluatedObservation{
			State:      capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_DEPENDENCY_UNAVAILABLE,
			Reason:     missingObservationReason,
		}
	}
	if _, available := AvailableObservation(snapshot, observation.GetKey(), now); available {
		return EvaluatedObservation{
			State:      capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
			Reason:     observation.GetReason(),
		}
	}
	if observation.GetState() == capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE {
		return EvaluatedObservation{
			State:      capabilityv1.CapabilityState_CAPABILITY_STATE_UNKNOWN,
			ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_EXPIRED,
			Reason:     "capability observation or one of its dependency proofs is no longer current",
		}
	}
	return EvaluatedObservation{
		State:      observation.GetState(),
		ReasonCode: observation.GetReasonCode(),
		Reason:     observation.GetReason(),
	}
}

// ObservationTransitionEvaluation describes the effective old and new sides
// of a semantically meaningful transition. Sampling timestamps and TTL
// extension alone are intentionally absent from the comparison.
type ObservationTransitionEvaluation struct {
	Previous EvaluatedObservation
	Current  EvaluatedObservation
}

// EvaluateObservationTransition is the shared transition evaluator used by
// axnoded and controld. Evidence subject changes, effective state changes, and
// bounded reason-code changes create history; reason text and refresh time do
// not.
func EvaluateObservationTransition(
	previousSnapshot *capabilityv1.CapabilitySnapshot,
	previous *capabilityv1.CapabilityObservation,
	previousAt time.Time,
	currentSnapshot *capabilityv1.CapabilitySnapshot,
	current *capabilityv1.CapabilityObservation,
	currentAt time.Time,
) (ObservationTransitionEvaluation, bool) {
	evaluation := ObservationTransitionEvaluation{
		Previous: EvaluateObservation(previousSnapshot, previous, previousAt),
		Current:  EvaluateObservation(currentSnapshot, current, currentAt),
	}
	changed := evaluation.Previous.State != evaluation.Current.State ||
		evaluation.Previous.ReasonCode != evaluation.Current.ReasonCode ||
		evidenceID(previous) != evidenceID(current)
	return evaluation, changed
}

func evidenceID(observation *capabilityv1.CapabilityObservation) string {
	if observation == nil || observation.GetEvidence() == nil {
		return ""
	}
	return observation.GetEvidence().GetEvidenceID()
}
