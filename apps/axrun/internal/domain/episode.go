package domain

import "time"

type EpisodeStatus string
type FailureClass string

const (
	EpisodeStatusPending   EpisodeStatus = "pending"
	EpisodeStatusRunning   EpisodeStatus = "running"
	EpisodeStatusVerifying EpisodeStatus = "verifying"
	EpisodeStatusCompleted EpisodeStatus = "completed"
	EpisodeStatusFailed    EpisodeStatus = "failed"
)

const (
	FailureClassAgentFailed    FailureClass = "agent_failed"
	FailureClassVerifierFailed FailureClass = "verifier_failed"
	FailureClassInfrastructure FailureClass = "infrastructure"
	FailureClassPatchEmpty     FailureClass = "patch_empty"
	FailureClassPatchInvalid   FailureClass = "patch_invalid"
	FailureClassTimeout        FailureClass = "timeout"
)

// EpisodeTiming records wall-clock durations of each episode phase.
// WaitMS accounts for inter-phase overhead (context setup, trajectory
// writes, cleanup) that does not belong to any named phase.
type EpisodeTiming struct {
	SandboxCreateMS   int64 `json:"sandbox_create_ms,omitempty"`
	WorkspaceUploadMS int64 `json:"workspace_upload_ms,omitempty"`
	AgentExecMS       int64 `json:"agent_exec_ms,omitempty"`
	VerifierExecMS    int64 `json:"verifier_exec_ms,omitempty"`
	TotalMS           int64 `json:"total_ms,omitempty"`
	WaitMS            int64 `json:"wait_ms,omitempty"`
}

type Episode struct {
	ID                   string               `json:"id"`
	RunID                string               `json:"run_id"`
	TaskID               string               `json:"task_id"`
	AttemptIndex         int                  `json:"attempt_index"`
	Status               EpisodeStatus        `json:"status"`
	StartedAt            *time.Time           `json:"started_at,omitempty"`
	FinishedAt           *time.Time           `json:"finished_at,omitempty"`
	CompletedAt          *time.Time           `json:"completed_at,omitempty"`
	DurationMS           int64                `json:"duration_ms,omitempty"`
	FailureClass         FailureClass         `json:"failure_class,omitempty"`
	BaseRevision         string               `json:"base_revision,omitempty"`
	Agent                AgentSpec            `json:"agent"`
	Model                ModelSpec            `json:"model"`
	Sandbox              SandboxSpec          `json:"sandbox"`
	SandboxState         *SandboxRuntimeState `json:"sandbox_state,omitempty"`
	Timing               *EpisodeTiming       `json:"timing,omitempty"`
	Usage                *UsageMetrics        `json:"usage,omitempty"`
	Cost                 *CostMetrics         `json:"cost,omitempty"`
	TrajectoryPath       string               `json:"trajectory_path"`
	AgentResultPath      string               `json:"agent_result_path,omitempty"`
	VerifierResultPath   string               `json:"verifier_result_path"`
	RewardPath           string               `json:"reward_path"`
	ArtifactDir          string               `json:"artifact_dir"`
	ArtifactManifestPath string               `json:"artifact_manifest_path,omitempty"`
	Artifacts            []ArtifactRef        `json:"artifacts,omitempty"`
}
