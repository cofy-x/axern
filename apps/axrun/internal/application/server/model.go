package server

import (
	"fmt"
	"strings"

	rolloutapp "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
)

// RolloutRequest is the JSON body accepted by POST /v1/rollouts.
// All fields mirror rolloutapp.Params but use JSON-idiomatic names.
type RolloutRequest struct {
	TaskSetRef          string   `json:"task_set_ref,omitempty"`
	Agent               string   `json:"agent"`
	AgentImage          string   `json:"agent_image,omitempty"`
	AgentProfile        string   `json:"agent_profile,omitempty"`
	AgentApprovalPolicy string   `json:"agent_approval_policy,omitempty"`
	AgentCommand        string   `json:"agent_command,omitempty"`
	AgentCWD            string   `json:"agent_cwd,omitempty"`
	AgentUser           string   `json:"agent_user,omitempty"`
	AgentTimeoutSec     int      `json:"agent_timeout_sec,omitempty"`
	AgentMaxTurns       int      `json:"agent_max_turns,omitempty"`
	AgentOutputFormat   string   `json:"agent_output_format,omitempty"`
	AgentAllowedTools   []string `json:"agent_allowed_tools,omitempty"`
	AgentIdleTimeoutSec int      `json:"agent_idle_timeout_sec,omitempty"`
	AgentPatchPath      string   `json:"agent_patch_path,omitempty"`
	AgentPatchRequired  bool     `json:"agent_patch_required,omitempty"`
	AgentEnv            []string `json:"agent_env,omitempty"`
	Model               string   `json:"model,omitempty"`
	RuntimeClass        string   `json:"runtime_class,omitempty"`
	RunID               string   `json:"run_id,omitempty"`
	ResumeRunDir        string   `json:"resume_run_dir,omitempty"`
	BackendName         string   `json:"backend,omitempty"`
	Concurrency         int      `json:"concurrency,omitempty"`
	Attempts            int      `json:"attempts,omitempty"`
	TaskLimit           int      `json:"task_limit,omitempty"`
	SelectedTaskIDs     []string `json:"selected_task_ids,omitempty"`
	ShardIndex          int      `json:"shard_index,omitempty"`
	ShardCount          int      `json:"shard_count,omitempty"`
	Output              string   `json:"output,omitempty"`
	Execute             *bool    `json:"execute,omitempty"`
}

func (r RolloutRequest) validate() error {
	if strings.TrimSpace(r.ResumeRunDir) != "" {
		return nil
	}
	if strings.TrimSpace(r.Agent) == "" {
		return fmt.Errorf("agent is required")
	}
	if strings.TrimSpace(r.TaskSetRef) == "" {
		return fmt.Errorf("task_set_ref is required")
	}
	return nil
}

func (r RolloutRequest) toParams() rolloutapp.Params {
	execute := true
	if r.Execute != nil {
		execute = *r.Execute
	}
	concurrency := r.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	return rolloutapp.Params{
		TaskSetRef:          r.TaskSetRef,
		Agent:               r.Agent,
		AgentImage:          r.AgentImage,
		AgentProfile:        r.AgentProfile,
		AgentApprovalPolicy: r.AgentApprovalPolicy,
		AgentCommand:        r.AgentCommand,
		AgentCWD:            r.AgentCWD,
		AgentUser:           r.AgentUser,
		AgentTimeoutSec:     r.AgentTimeoutSec,
		AgentMaxTurns:       r.AgentMaxTurns,
		AgentOutputFormat:   r.AgentOutputFormat,
		AgentAllowedTools:   r.AgentAllowedTools,
		AgentIdleTimeoutSec: r.AgentIdleTimeoutSec,
		AgentPatchPath:      r.AgentPatchPath,
		AgentPatchRequired:  r.AgentPatchRequired,
		AgentEnv:            r.AgentEnv,
		Model:               r.Model,
		RuntimeClass:        r.RuntimeClass,
		RunID:               r.RunID,
		ResumeRunDir:        r.ResumeRunDir,
		BackendName:         r.BackendName,
		Concurrency:         concurrency,
		Attempts:            r.Attempts,
		TaskLimit:           r.TaskLimit,
		SelectedTaskIDs:     r.SelectedTaskIDs,
		ShardIndex:          r.ShardIndex,
		ShardCount:          r.ShardCount,
		Output:              r.Output,
		Execute:             execute,
	}
}
