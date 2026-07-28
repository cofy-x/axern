package rolloutspec

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	approllout "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	axernbackend "github.com/cofy-x/axern/apps/axrun/internal/backend/axern"
	"github.com/cofy-x/axern/lib/go/clientconfig"
	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"gopkg.in/yaml.v3"
	"os"
)

const APIVersion = "axrun/v1"

type Envelope struct {
	APIVersion string   `json:"api_version" yaml:"api_version"`
	Kind       string   `json:"kind" yaml:"kind"`
	Metadata   Metadata `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       Spec     `json:"spec" yaml:"spec"`
	Path       string   `json:"-" yaml:"-"`
}

type Metadata struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

type Spec struct {
	TaskSet   TaskSetRef        `json:"task_set" yaml:"task_set"`
	Agent     Agent             `json:"agent" yaml:"agent"`
	Model     string            `json:"model,omitempty" yaml:"model,omitempty"`
	Execution Execution         `json:"execution" yaml:"execution"`
	Selection Selection         `json:"selection,omitempty" yaml:"selection,omitempty"`
	Budget    Budget            `json:"budget,omitempty" yaml:"budget,omitempty"`
	OutputDir string            `json:"output_dir,omitempty" yaml:"output_dir,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

type TaskSetRef struct {
	Ref string `json:"ref" yaml:"ref"`
}

type Agent struct {
	Name               string            `json:"name" yaml:"name"`
	Runtime            AgentRuntime      `json:"runtime" yaml:"runtime"`
	Profile            string            `json:"profile,omitempty" yaml:"profile,omitempty"`
	ApprovalPolicy     string            `json:"approval_policy,omitempty" yaml:"approval_policy,omitempty"`
	Command            string            `json:"command,omitempty" yaml:"command,omitempty"`
	CWD                string            `json:"cwd,omitempty" yaml:"cwd,omitempty"`
	User               string            `json:"user,omitempty" yaml:"user,omitempty"`
	TimeoutSeconds     int               `json:"timeout_seconds,omitempty" yaml:"timeout_seconds,omitempty"`
	MaxTurns           int               `json:"max_turns,omitempty" yaml:"max_turns,omitempty"`
	OutputFormat       string            `json:"output_format,omitempty" yaml:"output_format,omitempty"`
	AllowedTools       []string          `json:"allowed_tools,omitempty" yaml:"allowed_tools,omitempty"`
	IdleTimeoutSeconds int               `json:"idle_timeout_seconds,omitempty" yaml:"idle_timeout_seconds,omitempty"`
	PatchPath          string            `json:"patch_path,omitempty" yaml:"patch_path,omitempty"`
	PatchRequired      bool              `json:"patch_required,omitempty" yaml:"patch_required,omitempty"`
	Env                map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

type AgentRuntime struct {
	Kind  string `json:"kind" yaml:"kind"`
	Image string `json:"image,omitempty" yaml:"image,omitempty"`
}

type Execution struct {
	Runner       string `json:"runner,omitempty" yaml:"runner,omitempty"`
	Namespace    string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	RuntimeClass string `json:"runtime_class,omitempty" yaml:"runtime_class,omitempty"`
	Concurrency  int    `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	Attempts     int    `json:"attempts,omitempty" yaml:"attempts,omitempty"`
}

type Selection struct {
	TaskIDs    []string `json:"task_ids,omitempty" yaml:"task_ids,omitempty"`
	Limit      int      `json:"limit,omitempty" yaml:"limit,omitempty"`
	ShardIndex int      `json:"shard_index,omitempty" yaml:"shard_index,omitempty"`
	ShardCount int      `json:"shard_count,omitempty" yaml:"shard_count,omitempty"`
}

type Budget struct {
	MaxWallTime     string `json:"max_wall_time,omitempty" yaml:"max_wall_time,omitempty"`
	MaxEpisodes     int    `json:"max_episodes,omitempty" yaml:"max_episodes,omitempty"`
	MaxTokens       int64  `json:"max_tokens,omitempty" yaml:"max_tokens,omitempty"`
	MaxCostMicrousd int64  `json:"max_cost_microusd,omitempty" yaml:"max_cost_microusd,omitempty"`
}

type Overrides struct {
	Runner      string
	Concurrency int
	Attempts    int
	OutputDir   string
	Context     *clientconfig.Context
}

func (e Envelope) Runner(override string) string { return first(override, e.Spec.Execution.Runner) }

func Load(path string) (*Envelope, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var envelope Envelope
	if strings.EqualFold(filepath.Ext(path), ".json") {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&envelope)
		if err == nil && !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
			err = fmt.Errorf("multiple JSON values")
		}
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		err = decoder.Decode(&envelope)
		if err == nil && !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
			err = fmt.Errorf("multiple YAML documents")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("parse rollout spec %q: %w", path, err)
	}
	envelope.Path = path
	if err := envelope.Validate(); err != nil {
		return nil, err
	}
	return &envelope, nil
}

func (e *Envelope) Validate() error {
	if e.APIVersion != APIVersion || e.Kind != "Rollout" {
		return fmt.Errorf("rollout spec requires api_version %q and kind Rollout", APIVersion)
	}
	if strings.TrimSpace(e.Spec.TaskSet.Ref) == "" || strings.TrimSpace(e.Spec.Agent.Name) == "" {
		return fmt.Errorf("spec.task_set.ref and spec.agent.name are required")
	}
	if e.Spec.Execution.Runner == "" {
		e.Spec.Execution.Runner = string(backend.NameAxern)
	}
	if err := backend.ValidateName(e.Spec.Execution.Runner); err != nil {
		return err
	}
	if e.Spec.Execution.Concurrency == 0 {
		e.Spec.Execution.Concurrency = 1
	}
	if e.Spec.Execution.Attempts == 0 {
		e.Spec.Execution.Attempts = 1
	}
	if e.Spec.Execution.Concurrency < 1 || e.Spec.Execution.Attempts < 1 {
		return fmt.Errorf("spec.execution.concurrency and attempts must be at least 1")
	}
	if e.Spec.Selection.Limit < 0 || e.Spec.Selection.ShardIndex < 0 || e.Spec.Selection.ShardCount < 0 {
		return fmt.Errorf("spec.selection values must be non-negative")
	}
	if e.Spec.Budget.MaxEpisodes < 0 || e.Spec.Budget.MaxTokens < 0 || e.Spec.Budget.MaxCostMicrousd < 0 {
		return fmt.Errorf("spec.budget values must be non-negative")
	}
	if e.Spec.Budget.MaxWallTime != "" {
		duration, err := time.ParseDuration(e.Spec.Budget.MaxWallTime)
		if err != nil || duration <= 0 {
			return fmt.Errorf("spec.budget.max_wall_time must be a positive duration")
		}
	}
	if e.Spec.Selection.ShardCount == 0 && e.Spec.Selection.ShardIndex != 0 || e.Spec.Selection.ShardCount > 0 && e.Spec.Selection.ShardIndex >= e.Spec.Selection.ShardCount {
		return fmt.Errorf("spec.selection shard index is invalid")
	}
	if e.Spec.OutputDir == "" {
		e.Spec.OutputDir = ".axrun/runs"
	}
	if err := validateAgent(e.Spec.Agent, e.Spec.Model, e.Spec.Execution.Runner); err != nil {
		return err
	}
	return nil
}

func (e Envelope) ControlRequest(idempotencyKey string) (*rolloutv1.CreateRolloutRequest, error) {
	if !strings.Contains(e.Spec.TaskSet.Ref, "@sha256:") {
		return nil, fmt.Errorf("managed rollout requires spec.task_set.ref to use an immutable sha256 digest")
	}
	budget := &rolloutv1.RolloutBudget{
		MaxEpisodes:     int32(e.Spec.Budget.MaxEpisodes),
		MaxTokens:       e.Spec.Budget.MaxTokens,
		MaxCostMicrousd: e.Spec.Budget.MaxCostMicrousd,
	}
	if e.Spec.Budget.MaxWallTime != "" {
		duration, _ := time.ParseDuration(e.Spec.Budget.MaxWallTime)
		budget.MaxWallTime = durationpb.New(duration)
	}
	if budget.GetMaxWallTime() == nil && budget.GetMaxEpisodes() == 0 && budget.GetMaxTokens() == 0 && budget.GetMaxCostMicrousd() == 0 {
		budget = nil
	}
	return &rolloutv1.CreateRolloutRequest{
		Namespace:      first(e.Spec.Execution.Namespace, "default"),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		Labels:         e.Spec.Labels,
		Spec: &rolloutv1.RolloutSpec{
			TaskSetRef: e.Spec.TaskSet.Ref,
			Agent: &rolloutv1.RolloutAgent{
				Name:           e.Spec.Agent.Name,
				Image:          e.Spec.Agent.Runtime.Image,
				Profile:        e.Spec.Agent.Profile,
				ApprovalPolicy: e.Spec.Agent.ApprovalPolicy,
				Command:        e.Spec.Agent.Command,
			},
			Model: e.Spec.Model,
			Execution: &rolloutv1.RolloutExecution{
				RuntimeClass: e.Spec.Execution.RuntimeClass,
				Concurrency:  int32(e.Spec.Execution.Concurrency),
				Attempts:     int32(e.Spec.Execution.Attempts),
			},
			Selection: &rolloutv1.TaskSetSelection{
				TaskIds:    append([]string(nil), e.Spec.Selection.TaskIDs...),
				Limit:      int32(e.Spec.Selection.Limit),
				ShardIndex: int32(e.Spec.Selection.ShardIndex),
				ShardCount: int32(e.Spec.Selection.ShardCount),
			},
			Budget: budget,
		},
	}, nil
}

func validateAgent(agent Agent, model, runner string) error {
	if agent.Runtime.Kind == "agent_image" && strings.TrimSpace(agent.Runtime.Image) == "" {
		return fmt.Errorf("spec.agent.runtime.image is required for agent_image")
	}
	if agent.Runtime.Kind == "agent_image" && !strings.Contains(agent.Runtime.Image, "@sha256:") {
		return fmt.Errorf("spec.agent.runtime.image must use an immutable sha256 digest reference")
	}
	if agent.Runtime.Kind != "" && agent.Runtime.Kind != "agent_image" && agent.Runtime.Kind != "sandbox_command" {
		return fmt.Errorf("spec.agent.runtime.kind must be agent_image or sandbox_command")
	}
	switch agent.Name {
	case "command":
		if strings.TrimSpace(agent.Command) == "" {
			return fmt.Errorf("spec.agent.command is required for command agent")
		}
		if agent.Profile != "" || agent.ApprovalPolicy != "" || model != "" || agent.Runtime.Image != "" {
			return fmt.Errorf("command agent does not accept profile, approval_policy, model, or image")
		}
	case "claude-code", "codex":
		if agent.Command != "" || strings.TrimSpace(agent.Profile) == "" || strings.TrimSpace(model) == "" {
			return fmt.Errorf("managed agent %q requires profile and model and does not accept command", agent.Name)
		}
		if agent.ApprovalPolicy != "never" && agent.ApprovalPolicy != "on_request" {
			return fmt.Errorf("managed agent %q requires approval_policy never or on_request", agent.Name)
		}
		if runner == string(backend.NameAxern) && agent.ApprovalPolicy != "never" {
			return fmt.Errorf("Axern runner requires managed agent approval_policy never")
		}
		if runner == string(backend.NameLocal) && agent.ApprovalPolicy == "never" {
			return fmt.Errorf("local runner does not allow managed agent approval_policy never")
		}
	default:
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("spec.model is required for agent %q", agent.Name)
		}
	}
	return nil
}

func (e Envelope) Params(execute bool, overrides Overrides) (approllout.Params, error) {
	taskSetRef := e.Spec.TaskSet.Ref
	if !strings.Contains(taskSetRef, "@sha256:") && !filepath.IsAbs(taskSetRef) {
		var err error
		taskSetRef, err = filepath.Abs(filepath.Join(filepath.Dir(e.Path), taskSetRef))
		if err != nil {
			return approllout.Params{}, err
		}
	}
	runner := e.Runner(overrides.Runner)
	outputDir := strings.TrimSpace(overrides.OutputDir)
	if outputDir == "" {
		outputDir = e.Spec.OutputDir
		if !filepath.IsAbs(outputDir) {
			outputDir = filepath.Join(filepath.Dir(e.Path), outputDir)
		}
	}
	params := approllout.Params{
		TaskSetRef:          taskSetRef,
		Agent:               e.Spec.Agent.Name,
		AgentImage:          e.Spec.Agent.Runtime.Image,
		AgentProfile:        e.Spec.Agent.Profile,
		AgentApprovalPolicy: e.Spec.Agent.ApprovalPolicy,
		AgentCommand:        e.Spec.Agent.Command,
		AgentCWD:            e.Spec.Agent.CWD,
		AgentUser:           e.Spec.Agent.User,
		AgentTimeoutSec:     e.Spec.Agent.TimeoutSeconds,
		AgentMaxTurns:       e.Spec.Agent.MaxTurns,
		AgentOutputFormat:   e.Spec.Agent.OutputFormat,
		AgentAllowedTools:   append([]string(nil), e.Spec.Agent.AllowedTools...),
		AgentIdleTimeoutSec: e.Spec.Agent.IdleTimeoutSeconds,
		AgentPatchPath:      e.Spec.Agent.PatchPath,
		AgentPatchRequired:  e.Spec.Agent.PatchRequired,
		AgentEnv:            envFlags(e.Spec.Agent.Env),
		Model:               e.Spec.Model,
		RuntimeClass:        e.Spec.Execution.RuntimeClass,
		RunID:               e.Metadata.Name,
		SelectedTaskIDs:     append([]string(nil), e.Spec.Selection.TaskIDs...),
		TaskLimit:           e.Spec.Selection.Limit,
		ShardIndex:          e.Spec.Selection.ShardIndex,
		ShardCount:          e.Spec.Selection.ShardCount,
		Execute:             execute,
		BackendName:         runner,
		Concurrency:         chooseInt(overrides.Concurrency, e.Spec.Execution.Concurrency),
		Attempts:            chooseInt(overrides.Attempts, e.Spec.Execution.Attempts),
		Output:              outputDir,
	}
	if runner == string(backend.NameAxern) {
		if overrides.Context == nil {
			return approllout.Params{}, fmt.Errorf("Axern runner requires a resolved Axern context")
		}
		params.AxernConfig = &axernbackend.Config{
			Endpoint:      overrides.Context.Endpoint,
			Namespace:     e.Spec.Execution.Namespace,
			RuntimeClass:  params.RuntimeClass,
			TLSCACert:     overrides.Context.TLS.CACert,
			TLSCert:       overrides.Context.TLS.Cert,
			TLSKey:        overrides.Context.TLS.Key,
			TLSServerName: overrides.Context.TLS.ServerName,
			ProxyMode:     overrides.Context.ProxyMode,
		}
	}
	return approllout.NormalizeParams(params)
}

func envFlags(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+values[key])
	}
	return out
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func chooseInt(override, value int) int {
	if override != 0 {
		return override
	}
	return value
}
