package domain

import "time"

type RunStatus string

const (
	RunStatusCreated   RunStatus = "created"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

type AgentSpec struct {
	Name           string              `json:"name"`
	Version        string              `json:"version,omitempty"`
	Runtime        *AgentRuntimeSpec   `json:"runtime,omitempty"`
	Profile        string              `json:"profile,omitempty"`
	ApprovalPolicy AgentApprovalPolicy `json:"approval_policy,omitempty"`
	Capabilities   []string            `json:"capabilities,omitempty"`
}

type ModelSpec struct {
	ID             string `json:"id,omitempty"`
	Provider       string `json:"provider,omitempty"`
	EndpointFamily string `json:"endpoint_family,omitempty"`
	Effort         string `json:"effort,omitempty"`
	TokenBudget    int    `json:"token_budget,omitempty"`
}

type SandboxBackend string

const (
	SandboxBackendLocal SandboxBackend = "local"
	SandboxBackendAxern SandboxBackend = "axern"
)

type SandboxSpec struct {
	Backend       SandboxBackend            `json:"backend" yaml:"backend"`
	RuntimeClass  string                    `json:"runtime_class" yaml:"runtime_class"`
	RuntimeSource *SandboxRuntimeSourceSpec `json:"runtime_source,omitempty" yaml:"runtime_source,omitempty"`
	Workdir       string                    `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Env           map[string]string         `json:"env,omitempty" yaml:"env,omitempty"`
	Resources     *ResourceSpec             `json:"resources,omitempty" yaml:"resources,omitempty"`
}

type RolloutRun struct {
	SchemaVersion   string         `json:"schema_version,omitempty"`
	ID              string         `json:"id"`
	Status          RunStatus      `json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       *time.Time     `json:"updated_at,omitempty"`
	Input           *InputSpec     `json:"input,omitempty"`
	Agent           AgentSpec      `json:"agent"`
	Model           ModelSpec      `json:"model"`
	Sandbox         SandboxSpec    `json:"sandbox"`
	Concurrency     int            `json:"concurrency"`
	AttemptsPerTask int            `json:"attempts_per_task"`
	TaskIDs         []string       `json:"task_ids,omitempty"`
	Selection       *TaskSelection `json:"selection,omitempty"`
	Timeouts        *TimeoutPolicy `json:"timeouts,omitempty"`
	Resources       *ResourceSpec  `json:"resources,omitempty"`
	Summary         *RunSummary    `json:"summary,omitempty"`
	Metadata        KeyValue       `json:"metadata,omitempty"`
	OutputPath      string         `json:"output_path"`
}

type TaskSelection struct {
	RequestedTaskIDs  []string   `json:"requested_task_ids,omitempty"`
	Limit             int        `json:"limit,omitempty"`
	Shard             *TaskShard `json:"shard,omitempty"`
	ResolvedTaskCount int        `json:"resolved_task_count"`
	SelectedTaskCount int        `json:"selected_task_count"`
}

type TaskShard struct {
	Index int `json:"index"`
	Count int `json:"count"`
}

type RunSummary struct {
	TaskCount              int           `json:"task_count"`
	EpisodeCount           int           `json:"episode_count"`
	PendingEpisodes        int           `json:"pending_episodes,omitempty"`
	RunningEpisodes        int           `json:"running_episodes,omitempty"`
	VerifyingEpisodes      int           `json:"verifying_episodes,omitempty"`
	CompletedEpisodes      int           `json:"completed_episodes,omitempty"`
	FailedEpisodes         int           `json:"failed_episodes,omitempty"`
	AgentFailedEpisodes    int           `json:"agent_failed_episodes,omitempty"`
	VerifierFailedEpisodes int           `json:"verifier_failed_episodes,omitempty"`
	InfraFailures          int           `json:"infra_failures,omitempty"`
	PatchEmptyEpisodes     int           `json:"patch_empty_episodes,omitempty"`
	PatchInvalidEpisodes   int           `json:"patch_invalid_episodes,omitempty"`
	TimeoutEpisodes        int           `json:"timeout_episodes,omitempty"`
	TotalDurationMS        int64         `json:"total_duration_ms,omitempty"`
	MeanEpisodeDurationMS  int64         `json:"mean_episode_duration_ms,omitempty"`
	TotalUsage             *UsageMetrics `json:"total_usage,omitempty"`
	TotalCost              *CostMetrics  `json:"total_cost,omitempty"`
}
