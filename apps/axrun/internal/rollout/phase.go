package rollout

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func (s *executionSession) emitPhase(phase domain.RolloutPhase, status domain.PhaseStatus, start time.Time, err error) {
	if s.request.PhaseReporter == nil {
		return
	}
	event := domain.PhaseEvent{
		RunID:        s.episode.RunID,
		Phase:        phase,
		Status:       status,
		EpisodeID:    s.episode.ID,
		TaskID:       s.episode.TaskID,
		AttemptIndex: s.episode.AttemptIndex,
		DurationMS:   phaseDuration(start),
	}
	if err != nil {
		rolloutErr := phaseEventError(err, phase)
		event.Error = &rolloutErr
	}
	s.request.PhaseReporter(event)
}

func phaseDuration(start time.Time) int64 {
	if start.IsZero() {
		return 0
	}
	return time.Since(start).Milliseconds()
}

func phaseEventError(err error, phase domain.RolloutPhase) domain.RolloutError {
	if err == nil {
		return domain.RolloutError{}
	}
	component := "rollout"
	code := domain.RolloutErrorInfrastructureFailure
	retriable := true
	switch phase {
	case domain.RolloutPhaseSandboxCreating:
		component = "sandbox"
		code = domain.RolloutErrorSandboxCreateFailed
	case domain.RolloutPhaseAgentRunning:
		component = "agent"
		code = domain.RolloutErrorAgentFailed
		retriable = false
	case domain.RolloutPhaseVerifying:
		component = "verifier"
		code = domain.RolloutErrorVerifierFailed
		retriable = false
	case domain.RolloutPhaseCollecting:
		component = "artifact"
		code = domain.RolloutErrorArtifactCaptureFailed
	}
	return domain.RolloutError{
		Code:      code,
		Message:   err.Error(),
		Phase:     phase,
		Component: component,
		Retriable: retriable,
	}
}
