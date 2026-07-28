package domain

import "time"

type AgentRawEventType string

const (
	AgentRawEventCommandStarted  AgentRawEventType = "agent.command_started"
	AgentRawEventCommandFinished AgentRawEventType = "agent.command_finished"
	AgentRawEventToolCall        AgentRawEventType = "agent.tool_call"
	AgentRawEventToolResult      AgentRawEventType = "agent.tool_result"
	AgentRawEventPatch           AgentRawEventType = "agent.patch"
	AgentRawEventArtifact        AgentRawEventType = "agent.artifact"
	AgentRawEventLLMRequest      AgentRawEventType = "llm.request"
	AgentRawEventLLMResponse     AgentRawEventType = "llm.response"
	AgentRawEventLLMChunk        AgentRawEventType = "llm.response_chunk"
	AgentRawEventLLMDone         AgentRawEventType = "llm.response_done"
	AgentRawEventLLMError        AgentRawEventType = "llm.error"
	AgentRawEventLLMTruncated    AgentRawEventType = "llm.telemetry_truncated"
)

type AgentRawEvent struct {
	EventID            string              `json:"event_id,omitempty"`
	Type               AgentRawEventType   `json:"type"`
	Timestamp          *time.Time          `json:"timestamp,omitempty"`
	Method             string              `json:"method,omitempty"`
	Path               string              `json:"path,omitempty"`
	Model              string              `json:"model,omitempty"`
	Status             int                 `json:"status,omitempty"`
	LatencyMS          int64               `json:"latency_ms,omitempty"`
	Headers            map[string][]string `json:"headers,omitempty"`
	BodyRef            string              `json:"body_ref,omitempty"`
	ChunkRef           string              `json:"chunk_ref,omitempty"`
	Error              string              `json:"error,omitempty"`
	DroppedEvents      int                 `json:"dropped_events,omitempty"`
	DroppedBodies      int                 `json:"dropped_bodies,omitempty"`
	DroppedBytes       int64               `json:"dropped_bytes,omitempty"`
	Usage              *UsageMetrics       `json:"usage,omitempty"`
	Cost               *CostMetrics        `json:"cost,omitempty"`
	RequestRef         string              `json:"request_ref,omitempty"`
	ResponseRef        string              `json:"response_ref,omitempty"`
	LauncherKind       AgentLauncherKind   `json:"launcher_kind,omitempty"`
	RuntimeType        AgentRuntimeType    `json:"runtime_type,omitempty"`
	RuntimeImage       string              `json:"runtime_image,omitempty"`
	RuntimeMountTarget string              `json:"runtime_mount_target,omitempty"`
	RuntimeBinDir      string              `json:"runtime_bin_dir,omitempty"`
	RuntimeProfile     string              `json:"runtime_profile,omitempty"`
	Command            []string            `json:"command,omitempty"`
	CommandText        string              `json:"command_text,omitempty"`
	CommandRef         string              `json:"command_ref,omitempty"`
	CWD                string              `json:"cwd,omitempty"`
	User               string              `json:"user,omitempty"`
	TimeoutSec         int                 `json:"timeout_sec,omitempty"`
	ExitCode           *int                `json:"exit_code,omitempty"`
	StdoutRef          string              `json:"stdout_ref,omitempty"`
	StderrRef          string              `json:"stderr_ref,omitempty"`
	ArtifactRef        string              `json:"artifact_ref,omitempty"`
	ArtifactKind       ArtifactKind        `json:"artifact_kind,omitempty"`
	PatchRef           string              `json:"patch_ref,omitempty"`
	ToolName           string              `json:"tool_name,omitempty"`
	ToolCallID         string              `json:"tool_call_id,omitempty"`
}
