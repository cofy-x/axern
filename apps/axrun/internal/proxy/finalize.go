package proxy

import "github.com/cofy-x/axern/apps/axrun/internal/domain"

// FinalizeAgentResult merges recorder-derived telemetry into an agent result.
// Harnesses should stay unaware of recorder storage details and let rollout
// call this once after harness.Run returns.
func FinalizeAgentResult(recorder *Recorder, result *domain.AgentResult) error {
	if recorder == nil || result == nil {
		return nil
	}
	recResult, err := recorder.Result()
	if err != nil {
		return err
	}

	if result.RawLogRef == "" {
		result.RawLogRef = recResult.RawLogRef
	}
	if recResult.Usage != nil && (result.Usage == nil || isUsageEmpty(result.Usage)) {
		result.Usage = recResult.Usage
	}
	if result.LLMRequestCount == 0 && recResult.RequestCount > 0 {
		result.LLMRequestCount = recResult.RequestCount
	}
	if result.LLMResponseCount == 0 && recResult.ResponseCount > 0 {
		result.LLMResponseCount = recResult.ResponseCount
	}
	if result.LLMErrorCount == 0 && recResult.ErrorCount > 0 {
		result.LLMErrorCount = recResult.ErrorCount
	}
	result.Artifacts = mergeArtifacts(result.Artifacts, recResult.Artifacts)

	if result.ExitReason != "" && result.ExitReason != domain.AgentExitReasonCompleted {
		return nil
	}
	if recResult.RequestCount == 0 && recResult.ResponseCount == 0 && recResult.ErrorCount == 0 {
		return nil
	}
	if recResult.RequestCount == 0 {
		result.ExitReason = domain.AgentExitReasonProxyNoRequests
		return nil
	}
	if recResult.ErrorCount > recResult.ResponseCount {
		result.ExitReason = domain.AgentExitReasonLLMError
	}
	return nil
}

func mergeArtifacts(existing []domain.ArtifactRef, incoming []domain.ArtifactRef) []domain.ArtifactRef {
	if len(incoming) == 0 {
		return existing
	}
	merged := append([]domain.ArtifactRef(nil), existing...)
	seen := make(map[string]int, len(existing))
	for idx, artifact := range merged {
		seen[artifactKey(artifact)] = idx
	}
	for _, artifact := range incoming {
		key := artifactKey(artifact)
		if idx, ok := seen[key]; ok {
			merged[idx] = mergeArtifactRef(merged[idx], artifact)
			continue
		}
		seen[key] = len(merged)
		merged = append(merged, artifact)
	}
	return merged
}

func artifactKey(artifact domain.ArtifactRef) string {
	return string(artifact.Kind) + "|" + artifact.Path
}

func isUsageEmpty(u *domain.UsageMetrics) bool {
	return u.InputTokens == 0 && u.OutputTokens == 0 && u.TotalTokens == 0 && u.ToolCalls == 0
}

func mergeArtifactRef(base domain.ArtifactRef, incoming domain.ArtifactRef) domain.ArtifactRef {
	if base.Description == "" {
		base.Description = incoming.Description
	}
	if base.SHA256 == "" {
		base.SHA256 = incoming.SHA256
	}
	if base.MediaType == "" {
		base.MediaType = incoming.MediaType
	}
	if base.SizeBytes == 0 {
		base.SizeBytes = incoming.SizeBytes
	}
	if base.CreatedAt == nil {
		base.CreatedAt = incoming.CreatedAt
	}
	if base.Producer == "" {
		base.Producer = incoming.Producer
	}
	if base.Role == "" {
		base.Role = incoming.Role
	}
	return base
}
