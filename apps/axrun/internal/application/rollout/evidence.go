package rollout

import (
	"errors"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func reportPhase(reporter domain.PhaseReporter, event domain.PhaseEvent) {
	if reporter == nil {
		return
	}
	reporter(event)
}

func reportRunPhase(params Params, runID string, phase domain.RolloutPhase, status domain.PhaseStatus, err error) {
	if runID == "" {
		runID = params.RunID
	}
	event := domain.PhaseEvent{
		RunID:  runID,
		Phase:  phase,
		Status: status,
	}
	if err != nil {
		rolloutErr := RolloutError(err, phase)
		event.Error = &rolloutErr
	}
	reportPhase(params.PhaseReporter, event)
}

func RolloutError(err error, phase domain.RolloutPhase) domain.RolloutError {
	if err == nil {
		return domain.RolloutError{}
	}
	var value domain.RolloutError
	if errors.As(err, &value) {
		return value
	}
	var pointer *domain.RolloutError
	if errors.As(err, &pointer) && pointer != nil {
		return *pointer
	}
	message := err.Error()
	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "runtime_source") && strings.Contains(lower, "requires"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorTaskRuntimeSourceMissing,
			Message:   message,
			Phase:     domain.RolloutPhasePreparingInputs,
			Component: "backend",
		}
	case strings.Contains(lower, "runtime image") || strings.Contains(lower, "dockerfile runtime_source") || strings.Contains(lower, "axern_axrun_image"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorRuntimeImagePrepareFailed,
			Message:   message,
			Phase:     domain.RolloutPhasePreparingInputs,
			Component: "runtime_image",
			Retriable: true,
		}
	case strings.Contains(lower, "validation") || strings.Contains(lower, "validate"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorValidationFailed,
			Message:   message,
			Phase:     domain.RolloutPhaseValidating,
			Component: "schema",
		}
	case strings.Contains(lower, "task") || strings.Contains(lower, "manifest") || strings.Contains(lower, "task source") || strings.Contains(lower, "input"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorInputResolutionFailed,
			Message:   message,
			Phase:     domain.RolloutPhasePlanning,
			Component: "input",
			Retriable: true,
		}
	case strings.Contains(lower, "artifact") || strings.Contains(lower, "patch") || strings.Contains(lower, "raw log") || strings.Contains(lower, "download"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorArtifactCaptureFailed,
			Message:   message,
			Phase:     domain.RolloutPhaseCollecting,
			Component: "artifact",
			Retriable: true,
		}
	case strings.Contains(lower, "timeout") && strings.Contains(lower, "verifier"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorVerifierTimeout,
			Message:   message,
			Phase:     domain.RolloutPhaseVerifying,
			Component: "verifier",
		}
	case strings.Contains(lower, "timeout") && strings.Contains(lower, "agent"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorAgentTimeout,
			Message:   message,
			Phase:     domain.RolloutPhaseAgentRunning,
			Component: "agent",
		}
	case strings.Contains(lower, "verifier"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorVerifierFailed,
			Message:   message,
			Phase:     domain.RolloutPhaseVerifying,
			Component: "verifier",
		}
	case strings.Contains(lower, "agent"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorAgentFailed,
			Message:   message,
			Phase:     domain.RolloutPhaseAgentRunning,
			Component: "agent",
		}
	case strings.Contains(lower, "sandbox"):
		return domain.RolloutError{
			Code:      domain.RolloutErrorSandboxCreateFailed,
			Message:   message,
			Phase:     domain.RolloutPhaseSandboxCreating,
			Component: "sandbox",
			Retriable: true,
		}
	}
	return domain.RolloutError{
		Code:      domain.RolloutErrorInfrastructureFailure,
		Message:   message,
		Phase:     phase,
		Component: "rollout",
		Retriable: true,
	}
}
