package exportdata

import "github.com/cofy-x/axern/apps/axrun/internal/domain"

func agentSummary(spec domain.AgentSpec) AgentSummary {
	summary := AgentSummary{
		Name:         spec.Name,
		Version:      spec.Version,
		Profile:      spec.Profile,
		Capabilities: copyStrings(spec.Capabilities),
	}
	if spec.Runtime != nil {
		summary.Runtime = agentRuntimeSummary(*spec.Runtime)
	}
	return summary
}

func agentRuntimeSummary(runtime domain.AgentRuntimeSpec) *AgentRuntimeSummary {
	return &AgentRuntimeSummary{
		Type:           runtime.Type,
		Image:          runtime.Image,
		MountTarget:    runtime.MountTarget,
		BinDir:         runtime.BinDir,
		Workdir:        runtime.Workdir,
		User:           runtime.User,
		TimeoutSec:     runtime.TimeoutSec,
		MaxTurns:       runtime.MaxTurns,
		OutputFormat:   runtime.OutputFormat,
		AllowedTools:   copyStrings(runtime.AllowedTools),
		IdleTimeoutSec: runtime.IdleTimeoutSec,
		Profile:        runtime.Profile,
		Prompt:         promptSummary(runtime.Prompt),
		Session:        sessionSummary(runtime.Session),
		Capabilities:   copyStrings(runtime.Capabilities),
		Artifacts:      artifactPolicySummary(runtime.Artifacts),
	}
}

func promptSummary(prompt *domain.PromptSpec) *PromptSummary {
	if prompt == nil {
		return nil
	}
	summary := &PromptSummary{
		Source:       prompt.Source,
		TemplatePath: prompt.TemplatePath,
		HasInline:    prompt.Inline != "",
	}
	if len(prompt.Rounds) > 0 {
		summary.Rounds = make([]PromptRoundSummary, 0, len(prompt.Rounds))
		for _, round := range prompt.Rounds {
			summary.Rounds = append(summary.Rounds, PromptRoundSummary{
				Index:             round.Index,
				Source:            round.Source,
				TemplatePath:      round.TemplatePath,
				HasInline:         round.Inline != "",
				RenderedPromptRef: round.RenderedPromptRef,
				ResumePrevious:    round.ResumePrevious,
				HasSessionID:      round.SessionID != "",
			})
		}
	}
	return summary
}

func sessionSummary(session *domain.AgentSessionSpec) *SessionSummary {
	if session == nil {
		return nil
	}
	return &SessionSummary{
		Mode:         session.Mode,
		HasSessionID: session.SessionID != "",
	}
}

func artifactPolicySummary(policy *domain.ArtifactPolicySpec) *domain.ArtifactPolicySpec {
	if policy == nil {
		return nil
	}
	return &domain.ArtifactPolicySpec{
		PatchPath:     policy.PatchPath,
		PatchRequired: policy.PatchRequired,
		OutputPaths:   copyStrings(policy.OutputPaths),
		CaptureStdout: policy.CaptureStdout,
		CaptureStderr: policy.CaptureStderr,
		CaptureRawLog: policy.CaptureRawLog,
	}
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}
