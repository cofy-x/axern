package domain

import "time"

type AgentStatus string
type AgentExitReason string
type AgentLauncherKind string

const (
	AgentStatusPending   AgentStatus = "pending"
	AgentStatusRunning   AgentStatus = "running"
	AgentStatusCompleted AgentStatus = "completed"
	AgentStatusFailed    AgentStatus = "failed"
	AgentStatusSkipped   AgentStatus = "skipped"
)

const (
	AgentExitReasonCompleted        AgentExitReason = "completed"
	AgentExitReasonCommandNonzero   AgentExitReason = "command_nonzero"
	AgentExitReasonIdleTimeout      AgentExitReason = "idle_timeout"
	AgentExitReasonProxyNoRequests  AgentExitReason = "proxy_no_requests"
	AgentExitReasonLLMError         AgentExitReason = "llm_error"
	AgentExitReasonCompletedNoPatch AgentExitReason = "completed_no_patch"
	AgentExitReasonInfrastructure   AgentExitReason = "infrastructure_error"
	AgentExitReasonTimeout          AgentExitReason = "timeout"
)

const (
	AgentLauncherKindSandboxCommand AgentLauncherKind = "sandbox-command"
	AgentLauncherKindAgentImage     AgentLauncherKind = "agent-image"
)

type UsageMetrics struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
	ToolCalls    int `json:"tool_calls,omitempty"`
}

type CostMetrics struct {
	Amount   float64 `json:"amount,omitempty"`
	Currency string  `json:"currency,omitempty"`
}

// PatchValidation records the result of validating an agent-produced patch.
type PatchValidation struct {
	Source       string `json:"source"`
	Valid        bool   `json:"valid"`
	ApplyCheck   bool   `json:"apply_check"`
	ByteSize     int64  `json:"byte_size,omitempty"`
	SHA256       string `json:"sha256,omitempty"`
	HunkCount    int    `json:"hunk_count,omitempty"`
	FilesChanged int    `json:"files_changed,omitempty"`
	Error        string `json:"error,omitempty"`
}

type AgentResult struct {
	Status                 AgentStatus       `json:"status"`
	Summary                string            `json:"summary,omitempty"`
	Error                  string            `json:"error,omitempty"`
	ExitReason             AgentExitReason   `json:"exit_reason,omitempty"`
	LauncherKind           AgentLauncherKind `json:"launcher_kind,omitempty"`
	RuntimeType            AgentRuntimeType  `json:"runtime_type,omitempty"`
	RuntimeImage           string            `json:"runtime_image,omitempty"`
	RuntimeMountTarget     string            `json:"runtime_mount_target,omitempty"`
	RuntimeBinDir          string            `json:"runtime_bin_dir,omitempty"`
	RuntimeProfile         string            `json:"runtime_profile,omitempty"`
	ExitCode               *int              `json:"exit_code,omitempty"`
	Stdout                 string            `json:"stdout,omitempty"`
	StdoutRef              string            `json:"stdout_ref,omitempty"`
	Stderr                 string            `json:"stderr,omitempty"`
	StderrRef              string            `json:"stderr_ref,omitempty"`
	RawLogRef              string            `json:"raw_log_ref,omitempty"`
	PatchRef               string            `json:"patch_ref,omitempty"`
	PatchValidation        *PatchValidation  `json:"patch_validation,omitempty"`
	StartedAt              *time.Time        `json:"started_at,omitempty"`
	FinishedAt             *time.Time        `json:"finished_at,omitempty"`
	DurationMS             int64             `json:"duration_ms,omitempty"`
	Usage                  *UsageMetrics     `json:"usage,omitempty"`
	Cost                   *CostMetrics      `json:"cost,omitempty"`
	Artifacts              []ArtifactRef     `json:"artifacts,omitempty"`
	LLMRequestCount        int               `json:"llm_request_count,omitempty"`
	LLMResponseCount       int               `json:"llm_response_count,omitempty"`
	LLMErrorCount          int               `json:"llm_error_count,omitempty"`
	TrajectoryStepRefs     []int             `json:"trajectory_step_refs,omitempty"`
	ManagedProxyReportJSON []byte            `json:"-"`
}
