package managedrollout

import (
	"errors"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/command"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
)

func TestOutcomeUsesEpisodeFailureClass(t *testing.T) {
	err := Outcome(&rolloutv1.GetRolloutResponse{
		Rollout:  &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED},
		Episodes: []*rolloutv1.Episode{{FailureClass: rolloutv1.FailureClass_FAILURE_CLASS_METERING}},
	})
	var exit command.ExitError
	if !errors.As(err, &exit) || exit.Code != 12 {
		t.Fatalf("Outcome() = %#v", err)
	}
}

func TestOutcomeUsesRolloutFailureClassWhenPlanningHasNoEpisode(t *testing.T) {
	err := Outcome(&rolloutv1.GetRolloutResponse{
		Rollout: &rolloutv1.Rollout{
			Status:       rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED,
			FailureClass: rolloutv1.FailureClass_FAILURE_CLASS_BUDGET,
		},
	})
	var exit command.ExitError
	if !errors.As(err, &exit) || exit.Code != 12 {
		t.Fatalf("Outcome() = %#v", err)
	}
}

func TestOutcomeClassifiesAgentAndVerifierFailuresAsTaskFailure(t *testing.T) {
	for _, failure := range []rolloutv1.FailureClass{
		rolloutv1.FailureClass_FAILURE_CLASS_AGENT,
		rolloutv1.FailureClass_FAILURE_CLASS_VERIFIER,
	} {
		err := Outcome(&rolloutv1.GetRolloutResponse{
			Rollout:  &rolloutv1.Rollout{Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_FAILED},
			Episodes: []*rolloutv1.Episode{{FailureClass: failure}},
		})
		var exit command.ExitError
		if !errors.As(err, &exit) || exit.Code != 10 {
			t.Fatalf("Outcome(%s) = %#v", failure, err)
		}
	}
}
