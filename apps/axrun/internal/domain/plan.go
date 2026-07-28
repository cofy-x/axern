package domain

import "time"

type RolloutPlan struct {
	SchemaVersion   string               `json:"schema_version,omitempty"`
	RunID           string               `json:"run_id"`
	CreatedAt       time.Time            `json:"created_at"`
	Input           *InputSpec           `json:"input,omitempty"`
	Selection       TaskSelection        `json:"selection"`
	Concurrency     int                  `json:"concurrency"`
	AttemptsPerTask int                  `json:"attempts_per_task"`
	Agent           AgentSpec            `json:"agent"`
	Provider        *ProviderRequirement `json:"provider,omitempty"`
	Model           ModelSpec            `json:"model,omitempty"`
	Sandbox         SandboxSpec          `json:"sandbox"`
	TaskIDs         []string             `json:"task_ids"`
	Episodes        []PlannedEpisode     `json:"episodes"`
}

type ProviderRequirement struct {
	WireAPI string `json:"wire_api"`
}

type PlannedEpisode struct {
	ID           string `json:"id"`
	TaskID       string `json:"task_id"`
	AttemptIndex int    `json:"attempt_index"`
	Order        int    `json:"order"`
}
