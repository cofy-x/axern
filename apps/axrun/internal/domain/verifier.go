package domain

import "time"

type RewardStatus string

const (
	RewardStatusPending     RewardStatus = "pending"
	RewardStatusScored      RewardStatus = "scored"
	RewardStatusUnscored    RewardStatus = "unscored"
	RewardStatusAgentFailed RewardStatus = "agent_failed"
	RewardStatusInvalid     RewardStatus = "invalid"
	RewardStatusInfraFailed RewardStatus = "infra_failed"
)

type VerifierType string

const (
	VerifierTypeNone  VerifierType = "none"
	VerifierTypeShell VerifierType = "shell"
)

type VerifierSpec struct {
	Type       VerifierType        `json:"type" yaml:"type"`
	Command    string              `json:"command,omitempty" yaml:"command,omitempty"`
	CWD        string              `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	TimeoutSec int                 `json:"timeout_sec,omitempty" yaml:"timeout_sec,omitempty"`
	Assets     []VerifierAssetSpec `json:"assets,omitempty" yaml:"assets,omitempty"`
}

type VerifierResult struct {
	Status     EpisodeStatus      `json:"status"`
	Type       VerifierType       `json:"type,omitempty"`
	Command    string             `json:"command,omitempty"`
	CWD        string             `json:"cwd,omitempty"`
	TimeoutSec int                `json:"timeout_sec,omitempty"`
	ExitCode   *int               `json:"exit_code,omitempty"`
	Stdout     string             `json:"stdout,omitempty"`
	Stderr     string             `json:"stderr,omitempty"`
	Error      string             `json:"error,omitempty"`
	StartedAt  *time.Time         `json:"started_at,omitempty"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
	DurationMS int64              `json:"duration_ms,omitempty"`
	Metadata   KeyValue           `json:"metadata,omitempty"`
	Metrics    map[string]float64 `json:"metrics,omitempty"`
	Artifacts  []ArtifactRef      `json:"artifacts,omitempty"`
}

type Reward struct {
	Status      RewardStatus       `json:"status"`
	Score       *float64           `json:"score,omitempty"`
	Passed      *bool              `json:"passed,omitempty"`
	Invalid     bool               `json:"invalid,omitempty"`
	Reason      string             `json:"reason,omitempty"`
	Diagnostics map[string]string  `json:"diagnostics,omitempty"`
	Metrics     map[string]float64 `json:"metrics,omitempty"`
	Final       bool               `json:"final"`
}
