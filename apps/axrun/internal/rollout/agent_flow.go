package rollout

import (
	"context"
	"fmt"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/reward"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type agentFlowRequest struct {
	store        Store
	paths        Paths
	task         domain.TaskInstance
	episode      domain.Episode
	sandbox      sandbox.Instance
	harness      agent.Harness
	trajectory   *trajectoryRecorder
	now          func() time.Time
	managedProxy *sandbox.ManagedProxyOptions
	recorder     *proxy.Recorder
	baseline     *WorkspaceBaseline
}

// agentFlowMetrics carries timing and usage data from the agent phase
// back to the harness for episode-level aggregation.
type agentFlowMetrics struct {
	DurationMS int64
	Usage      *domain.UsageMetrics
	Cost       *domain.CostMetrics
}

type agentFlowResult struct {
	Episode      domain.Episode
	ShouldVerify bool
	Metrics      agentFlowMetrics
}

func runAgentFlow(ctx context.Context, request agentFlowRequest) (agentFlowResult, error) {
	if request.harness == nil {
		result := domain.AgentResult{Status: domain.AgentStatusSkipped, Summary: "no agent harness configured"}
		if err := request.store.WriteAgentResult(request.paths.AgentJSONPath, result); err != nil {
			return agentFlowResult{Episode: request.episode}, err
		}
		return agentFlowResult{Episode: request.episode, ShouldVerify: true}, nil
	}

	episode := request.episode
	var stepRefs []int
	if index, err := request.trajectory.append(domain.TrajectoryStep{
		Type:     domain.TrajectoryEventAgentPlanned,
		Actor:    "rollout",
		Summary:  fmt.Sprintf("agent %q planned", episode.Agent.Name),
		Metadata: agentRuntimeSpecMetadata(episode.Agent),
	}); err != nil {
		return agentFlowResult{Episode: episode}, err
	} else {
		stepRefs = append(stepRefs, index)
	}
	if index, err := request.trajectory.append(domain.TrajectoryStep{
		Type:     domain.TrajectoryEventAgentStarted,
		Actor:    "rollout",
		Summary:  fmt.Sprintf("agent %q started", episode.Agent.Name),
		Metadata: agentRuntimeSpecMetadata(episode.Agent),
	}); err != nil {
		return agentFlowResult{Episode: episode}, err
	} else {
		stepRefs = append(stepRefs, index)
	}

	result, err := request.harness.Run(ctx, agent.Request{
		Agent:        episode.Agent,
		Model:        episode.Model,
		Task:         request.task,
		Episode:      episode,
		Sandbox:      request.sandbox,
		Instruction:  request.task.Instruction,
		ArtifactDir:  request.paths.ArtifactDir,
		ManagedProxy: request.managedProxy,
		Recorder:     request.recorder,
	})
	agentTimedOut := false
	if err != nil {
		if isTimeoutErr(err) {
			agentTimedOut = true
			result.Status = domain.AgentStatusFailed
			result.ExitReason = domain.AgentExitReasonTimeout
			result.Error = fmt.Sprintf("agent timed out: %v", err)
			request.trajectory.append(domain.TrajectoryStep{
				Type:    domain.TrajectoryEventSystemTimeout,
				Actor:   "rollout",
				Summary: "agent timeout exceeded",
				Metadata: domain.KeyValue{
					"source": "agent",
				},
			})
		} else {
			return agentFlowResult{Episode: episode}, err
		}
	}
	if !agentTimedOut {
		if err := captureAgentPatch(patchCaptureRequest{
			ctx:      ctx,
			store:    request.store,
			paths:    request.paths,
			sandbox:  request.sandbox,
			agent:    episode.Agent,
			baseline: request.baseline,
		}, &result); err != nil {
			return agentFlowResult{Episode: episode}, err
		}
	}
	if len(result.ManagedProxyReportJSON) > 0 {
		if err := proxy.ImportManagedProxyReport(request.recorder, result.ManagedProxyReportJSON); err != nil {
			return agentFlowResult{Episode: episode}, err
		}
	}
	if err := proxy.FinalizeAgentResult(request.recorder, &result); err != nil {
		return agentFlowResult{Episode: episode}, err
	}
	if result.Cost == nil && result.Usage != nil {
		result.Cost = proxy.EstimateCost(episode.Model.ID, result.Usage)
	}
	metrics := agentFlowMetrics{
		DurationMS: result.DurationMS,
		Usage:      result.Usage,
		Cost:       result.Cost,
	}
	shouldVerify, err := shouldVerifyAgentResult(result.Status)
	if err != nil {
		return agentFlowResult{Episode: episode, Metrics: metrics}, err
	}
	if err := captureAgentArtifacts(request.store, request.paths, &result); err != nil {
		return agentFlowResult{Episode: episode, Metrics: metrics}, err
	}
	summary := result.Summary
	if summary == "" {
		summary = fmt.Sprintf("agent finished with status %q", result.Status)
	}
	refs, err := appendAgentResultSteps(request.trajectory, episode.Agent.Name, result, summary)
	if err != nil {
		return agentFlowResult{Episode: episode, Metrics: metrics}, err
	}
	stepRefs = append(stepRefs, refs...)
	result.TrajectoryStepRefs = append(result.TrajectoryStepRefs, stepRefs...)
	if err := request.store.WriteAgentResult(request.paths.AgentJSONPath, result); err != nil {
		return agentFlowResult{Episode: episode, Metrics: metrics}, err
	}
	if len(result.Artifacts) > 0 {
		episode.Artifacts = append(episode.Artifacts, result.Artifacts...)
	}
	if !shouldVerify {
		episode.Status = domain.EpisodeStatusFailed
		episode.FailureClass = classifyAgentFailure(result)
		finishedAt := now(request.now)
		episode.FinishedAt = &finishedAt
		if err := request.store.WriteReward(request.paths.RewardJSONPath, reward.AgentFailed(summary)); err != nil {
			return agentFlowResult{Episode: episode, Metrics: metrics}, err
		}
		return agentFlowResult{Episode: episode, Metrics: metrics}, nil
	}
	return agentFlowResult{Episode: episode, ShouldVerify: true, Metrics: metrics}, nil
}

func shouldVerifyAgentResult(status domain.AgentStatus) (bool, error) {
	switch status {
	case domain.AgentStatusCompleted, domain.AgentStatusSkipped:
		return true, nil
	case domain.AgentStatusFailed:
		return false, nil
	default:
		return false, fmt.Errorf("agent returned unsupported final status %q", status)
	}
}

func appendAgentResultSteps(trajectory *trajectoryRecorder, agentName string, result domain.AgentResult, summary string) ([]int, error) {
	var refs []int
	if result.StdoutRef != "" {
		stdoutArtifact := artifactForPath(result.StdoutRef, domain.ArtifactKindAgentStdout, "agent stdout")
		index, err := trajectory.append(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventAgentStdout,
			Actor:     agentName,
			Summary:   "agent stdout captured",
			OutputRef: result.StdoutRef,
			Artifacts: []domain.ArtifactRef{stdoutArtifact},
		})
		if err != nil {
			return refs, err
		}
		refs = append(refs, index)
	}
	if result.StderrRef != "" {
		stderrArtifact := artifactForPath(result.StderrRef, domain.ArtifactKindAgentStderr, "agent stderr")
		index, err := trajectory.append(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventAgentStderr,
			Actor:     agentName,
			Summary:   "agent stderr captured",
			OutputRef: result.StderrRef,
			Artifacts: []domain.ArtifactRef{stderrArtifact},
		})
		if err != nil {
			return refs, err
		}
		refs = append(refs, index)
	}
	llmArtifacts := artifactRefsForKind(result.Artifacts, domain.ArtifactKindAgentRawLog, domain.ArtifactKindLLMTelemetry)
	if result.LLMRequestCount > 0 {
		index, err := trajectory.append(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventAgentLLMRequest,
			Actor:     agentName,
			Summary:   fmt.Sprintf("captured %d LLM request event(s)", result.LLMRequestCount),
			OutputRef: result.RawLogRef,
			RawRef:    result.RawLogRef,
			Artifacts: llmArtifacts,
		})
		if err != nil {
			return refs, err
		}
		refs = append(refs, index)
	}
	if result.LLMResponseCount > 0 {
		index, err := trajectory.append(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventAgentLLMResponse,
			Actor:     agentName,
			Summary:   fmt.Sprintf("captured %d LLM response event(s)", result.LLMResponseCount),
			OutputRef: result.RawLogRef,
			RawRef:    result.RawLogRef,
			Usage:     result.Usage,
			Artifacts: llmArtifacts,
		})
		if err != nil {
			return refs, err
		}
		refs = append(refs, index)
	}
	if result.LLMErrorCount > 0 {
		index, err := trajectory.append(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventAgentLLMError,
			Actor:     agentName,
			Summary:   fmt.Sprintf("captured %d LLM error event(s)", result.LLMErrorCount),
			OutputRef: result.RawLogRef,
			RawRef:    result.RawLogRef,
			Artifacts: llmArtifacts,
		})
		if err != nil {
			return refs, err
		}
		refs = append(refs, index)
	}
	if result.PatchRef != "" {
		patchArtifact := artifactForPath(result.PatchRef, domain.ArtifactKindPatch, "agent patch")
		index, err := trajectory.append(domain.TrajectoryStep{
			Type:      domain.TrajectoryEventPatchCreated,
			Actor:     agentName,
			Summary:   "agent patch captured",
			OutputRef: result.PatchRef,
			RawRef:    result.RawLogRef,
			Artifacts: []domain.ArtifactRef{patchArtifact},
		})
		if err != nil {
			return refs, err
		}
		refs = append(refs, index)
	}
	index, err := trajectory.append(domain.TrajectoryStep{
		Type:       domain.TrajectoryEventAgentFinished,
		Actor:      "rollout",
		Summary:    summary,
		ExitCode:   result.ExitCode,
		DurationMS: result.DurationMS,
		Usage:      result.Usage,
		Cost:       result.Cost,
		Artifacts:  result.Artifacts,
		Metadata:   agentLauncherMetadata(result),
	})
	if err != nil {
		return refs, err
	}
	return append(refs, index), nil
}

func classifyAgentFailure(result agent.Result) domain.FailureClass {
	if result.ExitReason == domain.AgentExitReasonTimeout {
		return domain.FailureClassTimeout
	}
	if result.PatchValidation != nil && !result.PatchValidation.Valid {
		return domain.FailureClassPatchInvalid
	}
	if result.ExitReason == domain.AgentExitReasonCompletedNoPatch {
		return domain.FailureClassPatchEmpty
	}
	return domain.FailureClassAgentFailed
}

func agentLauncherMetadata(result domain.AgentResult) domain.KeyValue {
	metadata := domain.KeyValue{}
	if result.LauncherKind != "" {
		metadata["launcher_kind"] = string(result.LauncherKind)
	}
	if result.RuntimeType != "" {
		metadata["runtime_type"] = string(result.RuntimeType)
	}
	if result.RuntimeImage != "" {
		metadata["runtime_image"] = result.RuntimeImage
	}
	if result.RuntimeMountTarget != "" {
		metadata["runtime_mount_target"] = result.RuntimeMountTarget
	}
	if result.RuntimeBinDir != "" {
		metadata["runtime_bin_dir"] = result.RuntimeBinDir
	}
	if result.RuntimeProfile != "" {
		metadata["runtime_profile"] = result.RuntimeProfile
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}

func agentRuntimeSpecMetadata(spec domain.AgentSpec) domain.KeyValue {
	metadata := domain.KeyValue{}
	if spec.Runtime != nil {
		if spec.Runtime.Type != "" {
			metadata["runtime_type"] = string(spec.Runtime.Type)
		}
		if spec.Runtime.Image != "" {
			metadata["runtime_image"] = spec.Runtime.Image
		}
		if spec.Runtime.MountTarget != "" {
			metadata["runtime_mount_target"] = spec.Runtime.MountTarget
		}
		if spec.Runtime.BinDir != "" {
			metadata["runtime_bin_dir"] = spec.Runtime.BinDir
		}
		if spec.Runtime.Profile != "" {
			metadata["runtime_profile"] = spec.Runtime.Profile
		}
	}
	if len(metadata) == 0 {
		return nil
	}
	return metadata
}
