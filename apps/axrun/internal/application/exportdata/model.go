package exportdata

import (
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type Format string

const (
	FormatSFT        Format = "sft"
	FormatReward     Format = "reward"
	FormatTrace      Format = "trace"
	FormatPreference Format = "preference"
)

type Params struct {
	RunDir     string
	OutputPath string
	Format     Format
}

type Result struct {
	Format      Format
	RunID       string
	OutputPath  string
	RecordCount int
}

type EpisodeRefs struct {
	RunDir               string `json:"run_dir"`
	TaskPath             string `json:"task_path,omitempty"`
	EpisodePath          string `json:"episode_path,omitempty"`
	AgentResultPath      string `json:"agent_result_path,omitempty"`
	VerifierResultPath   string `json:"verifier_result_path,omitempty"`
	RewardPath           string `json:"reward_path,omitempty"`
	TrajectoryPath       string `json:"trajectory_path,omitempty"`
	RawLogRef            string `json:"raw_log_ref,omitempty"`
	PatchRef             string `json:"patch_ref,omitempty"`
	ArtifactDir          string `json:"artifact_dir,omitempty"`
	ArtifactManifestPath string `json:"artifact_manifest_path,omitempty"`
	LLMTelemetryRef      string `json:"llm_telemetry_ref,omitempty"`
}

type AgentSummary struct {
	Name         string               `json:"name"`
	Version      string               `json:"version,omitempty"`
	Profile      string               `json:"profile,omitempty"`
	Runtime      *AgentRuntimeSummary `json:"runtime,omitempty"`
	Capabilities []string             `json:"capabilities,omitempty"`
}

type AgentRuntimeSummary struct {
	Type           domain.AgentRuntimeType    `json:"type,omitempty"`
	Image          string                     `json:"image,omitempty"`
	MountTarget    string                     `json:"mount_target,omitempty"`
	BinDir         string                     `json:"bin_dir,omitempty"`
	Workdir        string                     `json:"workdir,omitempty"`
	User           string                     `json:"user,omitempty"`
	TimeoutSec     int                        `json:"timeout_sec,omitempty"`
	MaxTurns       int                        `json:"max_turns,omitempty"`
	OutputFormat   string                     `json:"output_format,omitempty"`
	AllowedTools   []string                   `json:"allowed_tools,omitempty"`
	IdleTimeoutSec int                        `json:"idle_timeout_sec,omitempty"`
	Profile        string                     `json:"profile,omitempty"`
	Prompt         *PromptSummary             `json:"prompt,omitempty"`
	Session        *SessionSummary            `json:"session,omitempty"`
	Capabilities   []string                   `json:"capabilities,omitempty"`
	Artifacts      *domain.ArtifactPolicySpec `json:"artifacts,omitempty"`
}

type PromptSummary struct {
	Source       domain.PromptSourceType `json:"source,omitempty"`
	TemplatePath string                  `json:"template_path,omitempty"`
	HasInline    bool                    `json:"has_inline,omitempty"`
	Rounds       []PromptRoundSummary    `json:"rounds,omitempty"`
}

type PromptRoundSummary struct {
	Index             int                     `json:"index"`
	Source            domain.PromptSourceType `json:"source,omitempty"`
	TemplatePath      string                  `json:"template_path,omitempty"`
	HasInline         bool                    `json:"has_inline,omitempty"`
	RenderedPromptRef string                  `json:"rendered_prompt_ref,omitempty"`
	ResumePrevious    bool                    `json:"resume_previous,omitempty"`
	HasSessionID      bool                    `json:"has_session_id,omitempty"`
}

type SessionSummary struct {
	Mode         domain.AgentSessionMode `json:"mode,omitempty"`
	HasSessionID bool                    `json:"has_session_id,omitempty"`
}

type SFTRecord struct {
	SchemaVersion       string                 `json:"schema_version"`
	RecordID            string                 `json:"record_id"`
	SourceSchemaVersion string                 `json:"source_schema_version,omitempty"`
	RunID               string                 `json:"run_id"`
	EpisodeID           string                 `json:"episode_id"`
	TaskID              string                 `json:"task_id"`
	AttemptIndex        int                    `json:"attempt_index"`
	Agent               AgentSummary           `json:"agent"`
	Model               domain.ModelSpec       `json:"model"`
	Instruction         string                 `json:"instruction"`
	Assistant           string                 `json:"assistant"`
	EpisodeStatus       domain.EpisodeStatus   `json:"episode_status"`
	AgentStatus         domain.AgentStatus     `json:"agent_status,omitempty"`
	AgentExitReason     domain.AgentExitReason `json:"agent_exit_reason,omitempty"`
	Reward              RewardSummary          `json:"reward"`
	Usage               *domain.UsageMetrics   `json:"usage,omitempty"`
	Cost                *domain.CostMetrics    `json:"cost,omitempty"`
	DurationMS          int64                  `json:"duration_ms,omitempty"`
	Timing              *domain.EpisodeTiming  `json:"timing,omitempty"`
	FinishedAt          *time.Time             `json:"finished_at,omitempty"`
	Refs                EpisodeRefs            `json:"refs"`
	Metadata            domain.KeyValue        `json:"metadata,omitempty"`
}

type RewardRecord struct {
	SchemaVersion       string                 `json:"schema_version"`
	RecordID            string                 `json:"record_id"`
	SourceSchemaVersion string                 `json:"source_schema_version,omitempty"`
	RunID               string                 `json:"run_id"`
	EpisodeID           string                 `json:"episode_id"`
	TaskID              string                 `json:"task_id"`
	AttemptIndex        int                    `json:"attempt_index"`
	Agent               AgentSummary           `json:"agent"`
	Model               domain.ModelSpec       `json:"model"`
	Instruction         string                 `json:"instruction"`
	EpisodeStatus       domain.EpisodeStatus   `json:"episode_status"`
	AgentStatus         domain.AgentStatus     `json:"agent_status,omitempty"`
	AgentExitReason     domain.AgentExitReason `json:"agent_exit_reason,omitempty"`
	Verifier            VerifierSummary        `json:"verifier"`
	Reward              RewardSummary          `json:"reward"`
	Usage               *domain.UsageMetrics   `json:"usage,omitempty"`
	Cost                *domain.CostMetrics    `json:"cost,omitempty"`
	DurationMS          int64                  `json:"duration_ms,omitempty"`
	Timing              *domain.EpisodeTiming  `json:"timing,omitempty"`
	StartedAt           *time.Time             `json:"started_at,omitempty"`
	FinishedAt          *time.Time             `json:"finished_at,omitempty"`
	Refs                EpisodeRefs            `json:"refs"`
	Metadata            domain.KeyValue        `json:"metadata,omitempty"`
}

type TraceRecord struct {
	SchemaVersion       string                   `json:"schema_version"`
	RecordID            string                   `json:"record_id"`
	SourceSchemaVersion string                   `json:"source_schema_version,omitempty"`
	RunID               string                   `json:"run_id"`
	EpisodeID           string                   `json:"episode_id"`
	TaskID              string                   `json:"task_id"`
	AttemptIndex        int                      `json:"attempt_index"`
	Agent               AgentSummary             `json:"agent"`
	Model               domain.ModelSpec         `json:"model"`
	Source              string                   `json:"source"`
	Type                string                   `json:"type"`
	EventID             string                   `json:"event_id,omitempty"`
	ParentEventID       string                   `json:"parent_event_id,omitempty"`
	Index               int                      `json:"index,omitempty"`
	Line                int                      `json:"line,omitempty"`
	Timestamp           *time.Time               `json:"timestamp,omitempty"`
	Actor               string                   `json:"actor,omitempty"`
	Summary             string                   `json:"summary,omitempty"`
	Method              string                   `json:"method,omitempty"`
	Path                string                   `json:"path,omitempty"`
	Status              int                      `json:"status,omitempty"`
	LatencyMS           int64                    `json:"latency_ms,omitempty"`
	ModelID             string                   `json:"model_id,omitempty"`
	SourceRef           string                   `json:"source_ref,omitempty"`
	InputRef            string                   `json:"input_ref,omitempty"`
	OutputRef           string                   `json:"output_ref,omitempty"`
	PayloadRef          string                   `json:"payload_ref,omitempty"`
	RawRef              string                   `json:"raw_ref,omitempty"`
	BodyRef             string                   `json:"body_ref,omitempty"`
	ChunkRef            string                   `json:"chunk_ref,omitempty"`
	RequestRef          string                   `json:"request_ref,omitempty"`
	ResponseRef         string                   `json:"response_ref,omitempty"`
	CommandRef          string                   `json:"command_ref,omitempty"`
	CWD                 string                   `json:"cwd,omitempty"`
	User                string                   `json:"user,omitempty"`
	TimeoutSec          int                      `json:"timeout_sec,omitempty"`
	LauncherKind        domain.AgentLauncherKind `json:"launcher_kind,omitempty"`
	RuntimeType         domain.AgentRuntimeType  `json:"runtime_type,omitempty"`
	RuntimeImage        string                   `json:"runtime_image,omitempty"`
	RuntimeMountTarget  string                   `json:"runtime_mount_target,omitempty"`
	RuntimeBinDir       string                   `json:"runtime_bin_dir,omitempty"`
	RuntimeProfile      string                   `json:"runtime_profile,omitempty"`
	ExitCode            *int                     `json:"exit_code,omitempty"`
	StdoutRef           string                   `json:"stdout_ref,omitempty"`
	StderrRef           string                   `json:"stderr_ref,omitempty"`
	ArtifactRef         string                   `json:"artifact_ref,omitempty"`
	ArtifactKind        domain.ArtifactKind      `json:"artifact_kind,omitempty"`
	PatchRef            string                   `json:"patch_ref,omitempty"`
	ToolName            string                   `json:"tool_name,omitempty"`
	ToolCallID          string                   `json:"tool_call_id,omitempty"`
	Error               string                   `json:"error,omitempty"`
	DroppedEvents       int                      `json:"dropped_events,omitempty"`
	DroppedBodies       int                      `json:"dropped_bodies,omitempty"`
	DroppedBytes        int64                    `json:"dropped_bytes,omitempty"`
	Usage               *domain.UsageMetrics     `json:"usage,omitempty"`
	Cost                *domain.CostMetrics      `json:"cost,omitempty"`
	Artifacts           []domain.ArtifactRef     `json:"artifacts,omitempty"`
	Metadata            domain.KeyValue          `json:"metadata,omitempty"`
	Refs                EpisodeRefs              `json:"refs"`
}

// PreferenceRecord pairs a chosen (passing) episode with a rejected (failing)
// episode for the same task. It is produced when a run has multiple attempts
// per task and at least one attempt passes and one fails.
type PreferenceRecord struct {
	SchemaVersion       string          `json:"schema_version"`
	RecordID            string          `json:"record_id"`
	SourceSchemaVersion string          `json:"source_schema_version,omitempty"`
	RunID               string          `json:"run_id"`
	TaskID              string          `json:"task_id"`
	Instruction         string          `json:"instruction"`
	Chosen              EpisodeArm      `json:"chosen"`
	Rejected            EpisodeArm      `json:"rejected"`
	Metadata            domain.KeyValue `json:"metadata,omitempty"`
}

// EpisodeArm is one side of a preference pair.
type EpisodeArm struct {
	EpisodeID    string                 `json:"episode_id"`
	AttemptIndex int                    `json:"attempt_index"`
	Agent        AgentSummary           `json:"agent"`
	Model        domain.ModelSpec       `json:"model"`
	Assistant    string                 `json:"assistant,omitempty"`
	AgentStatus  domain.AgentStatus     `json:"agent_status,omitempty"`
	ExitReason   domain.AgentExitReason `json:"exit_reason,omitempty"`
	Reward       RewardSummary          `json:"reward"`
	DurationMS   int64                  `json:"duration_ms,omitempty"`
	Refs         EpisodeRefs            `json:"refs"`
}

type RewardSummary struct {
	Status domain.RewardStatus `json:"status"`
	Score  *float64            `json:"score,omitempty"`
	Passed *bool               `json:"passed,omitempty"`
	Final  bool                `json:"final"`
	Reason string              `json:"reason,omitempty"`
}

type VerifierSummary struct {
	Type     domain.VerifierType  `json:"type,omitempty"`
	Status   domain.EpisodeStatus `json:"status,omitempty"`
	ExitCode *int                 `json:"exit_code,omitempty"`
	Error    string               `json:"error,omitempty"`
}
