package managedrollout

import (
	"fmt"

	"github.com/cofy-x/axern/apps/axrun/internal/command"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
)

func Outcome(response *rolloutv1.GetRolloutResponse) error {
	if response == nil || response.GetRollout() == nil {
		return command.Exit(1, fmt.Errorf("rollout response is empty"))
	}
	rollout := response.GetRollout()
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_READY {
		return nil
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_CANCELLED {
		return command.Exit(13, fmt.Errorf("rollout was cancelled"))
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED && rollout.GetSummary().GetFailedEpisodes() == 0 && rollout.GetSummary().GetPassedEpisodes() == rollout.GetSummary().GetEpisodeCount() {
		return nil
	}
	for _, check := range rollout.GetPreflight().GetChecks() {
		if check.GetStatus() == rolloutv1.PreflightCheckStatus_PREFLIGHT_CHECK_STATUS_FAIL {
			return command.Exit(14, fmt.Errorf("rollout preflight was rejected: %s", check.GetMessage()))
		}
	}
	switch rollout.GetFailureClass() {
	case rolloutv1.FailureClass_FAILURE_CLASS_BUDGET, rolloutv1.FailureClass_FAILURE_CLASS_METERING:
		return command.Exit(12, fmt.Errorf("rollout budget or metering failed"))
	case rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE:
		return command.Exit(11, fmt.Errorf("rollout infrastructure failed"))
	case rolloutv1.FailureClass_FAILURE_CLASS_AGENT, rolloutv1.FailureClass_FAILURE_CLASS_VERIFIER:
		return command.Exit(10, fmt.Errorf("rollout completed with task failures"))
	}
	var budgetFailure, infrastructureFailure, taskFailure bool
	for _, episode := range response.GetEpisodes() {
		switch episode.GetFailureClass() {
		case rolloutv1.FailureClass_FAILURE_CLASS_BUDGET, rolloutv1.FailureClass_FAILURE_CLASS_METERING:
			budgetFailure = true
		case rolloutv1.FailureClass_FAILURE_CLASS_INFRASTRUCTURE:
			infrastructureFailure = true
		case rolloutv1.FailureClass_FAILURE_CLASS_AGENT, rolloutv1.FailureClass_FAILURE_CLASS_VERIFIER:
			taskFailure = true
		}
	}
	if budgetFailure {
		return command.Exit(12, fmt.Errorf("rollout budget or metering failed"))
	}
	if infrastructureFailure {
		return command.Exit(11, fmt.Errorf("rollout infrastructure failed"))
	}
	if taskFailure {
		return command.Exit(10, fmt.Errorf("rollout completed with task failures"))
	}
	if rollout.GetStatus() == rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED {
		return command.Exit(10, fmt.Errorf("rollout completed with task failures"))
	}
	return command.Exit(11, fmt.Errorf("rollout failed: %s", rollout.GetMessage()))
}
