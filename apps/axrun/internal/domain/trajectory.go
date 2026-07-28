package domain

import "time"

type TrajectoryEventType string

const (
	TrajectoryEventSystemResumeStarted     TrajectoryEventType = "system.resume_started"
	TrajectoryEventSystemSandboxStarting   TrajectoryEventType = "system.sandbox_starting"
	TrajectoryEventSystemSandboxStarted    TrajectoryEventType = "system.sandbox_started"
	TrajectoryEventSystemWorkspaceUpload   TrajectoryEventType = "system.workspace_uploaded"
	TrajectoryEventSystemImageBuildStart   TrajectoryEventType = "system.image_build_started"
	TrajectoryEventSystemImageBuildDone    TrajectoryEventType = "system.image_build_finished"
	TrajectoryEventSystemCleanupComplete   TrajectoryEventType = "system.cleanup_completed"
	TrajectoryEventSystemCleanupFailed     TrajectoryEventType = "system.cleanup_failed"
	TrajectoryEventSystemHealthCheckFailed TrajectoryEventType = "system.health_check_failed"
	TrajectoryEventSystemSandboxDeath      TrajectoryEventType = "system.sandbox_death"
	TrajectoryEventSystemInfraFailure      TrajectoryEventType = "system.infra_failure"
	TrajectoryEventSystemWorkspaceBaseline TrajectoryEventType = "system.workspace_baseline"
	TrajectoryEventSystemTimeout           TrajectoryEventType = "system.timeout"

	TrajectoryEventAgentPlanned     TrajectoryEventType = "agent.planned"
	TrajectoryEventAgentStarted     TrajectoryEventType = "agent.started"
	TrajectoryEventAgentStdout      TrajectoryEventType = "agent.stdout"
	TrajectoryEventAgentStderr      TrajectoryEventType = "agent.stderr"
	TrajectoryEventAgentFinished    TrajectoryEventType = "agent.finished"
	TrajectoryEventAgentToolCall    TrajectoryEventType = "agent.tool_call"
	TrajectoryEventAgentToolResult  TrajectoryEventType = "agent.tool_result"
	TrajectoryEventAgentLLMRequest  TrajectoryEventType = "agent.llm_request"
	TrajectoryEventAgentLLMResponse TrajectoryEventType = "agent.llm_response"
	TrajectoryEventAgentLLMError    TrajectoryEventType = "agent.llm_error"

	TrajectoryEventPatchCreated     TrajectoryEventType = "artifact.patch_created"
	TrajectoryEventArtifactCreated  TrajectoryEventType = "artifact.created"
	TrajectoryEventArtifactDownload TrajectoryEventType = "artifact.downloaded"

	TrajectoryEventVerifierPlanned  TrajectoryEventType = "verifier.planned"
	TrajectoryEventVerifierFinished TrajectoryEventType = "verifier.finished"
)

type TrajectoryStep struct {
	EventID       string              `json:"event_id,omitempty"`
	ParentEventID string              `json:"parent_event_id,omitempty"`
	Index         int                 `json:"index"`
	Timestamp     time.Time           `json:"timestamp"`
	Type          TrajectoryEventType `json:"type"`
	Actor         string              `json:"actor"`
	Summary       string              `json:"summary"`
	SourceRef     string              `json:"source_ref,omitempty"`
	InputRef      string              `json:"input_ref,omitempty"`
	OutputRef     string              `json:"output_ref,omitempty"`
	PayloadRef    string              `json:"payload_ref,omitempty"`
	RawRef        string              `json:"raw_ref,omitempty"`
	ExitCode      *int                `json:"exit_code,omitempty"`
	DurationMS    int64               `json:"duration_ms,omitempty"`
	Usage         *UsageMetrics       `json:"usage,omitempty"`
	Cost          *CostMetrics        `json:"cost,omitempty"`
	Artifacts     []ArtifactRef       `json:"artifacts,omitempty"`
	Metadata      KeyValue            `json:"metadata,omitempty"`
}
