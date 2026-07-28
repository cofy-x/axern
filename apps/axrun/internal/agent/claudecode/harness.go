package claudecode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

const (
	defaultCommand    = `claude -p --model "$AXRUN_MODEL_ID" "$AXRUN_TASK_INSTRUCTION"`
	defaultCWD        = "/home/axern"
	defaultBundleUser = "65532:65532"
	defaultBundleHome = "/tmp/axrun-claude-code"
	defaultTimeoutSec = 1800
)

type Harness struct {
	Config   Config
	Launcher agent.Launcher
}

func New(config Config) *Harness {
	return &Harness{Config: config}
}

func NewFromEnv() (*Harness, error) {
	config, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return New(config), nil
}

func (h *Harness) SetLauncher(launcher agent.Launcher) {
	h.Launcher = launcher
}

func (h *Harness) Preflight() error {
	if h.Config.TimeoutSec < 0 {
		return fmt.Errorf("claude-code agent timeout must be positive")
	}
	if h.Config.MaxTurns < 0 {
		return fmt.Errorf("claude-code agent max turns must be non-negative")
	}
	if h.Config.IdleTimeoutSec < 0 {
		return fmt.Errorf("claude-code agent idle timeout must be non-negative")
	}
	return nil
}

func (h *Harness) Run(ctx context.Context, request agent.Request) (agent.Result, error) {
	startedAt := time.Now().UTC()
	recorder := newCommandRecorder(request.Recorder)
	plan := h.launchPlan(request)
	if err := plan.Validate(); err != nil {
		return agent.Result{}, err
	}
	if err := h.writeRemoteConfig(request, &plan); err != nil {
		return agent.Result{}, err
	}
	launcher := h.launcherForRuntime(plan.RuntimeType)
	recorder.recordCommandStarted(plan)
	execResult, err := launcher.Launch(ctx, request.Sandbox, plan)
	finishedAt := time.Now().UTC()
	if err != nil {
		recorder.recordCommandFinished(plan, startedAt, nil, err)
		return agent.Result{}, err
	}
	exitCode := execResult.ExitCode
	recorder.recordCommandFinished(plan, startedAt, &exitCode, nil)
	status := domain.AgentStatusCompleted
	summary := "claude-code agent completed"
	errorText := ""
	exitReason := domain.AgentExitReasonCompleted
	if exitCode != 0 {
		status = domain.AgentStatusFailed
		summary = fmt.Sprintf("claude-code agent exited with status %d", exitCode)
		errorText = summary
		exitReason = domain.AgentExitReasonCommandNonzero
	}
	result := agent.Result{
		Status:                 status,
		Summary:                summary,
		Error:                  errorText,
		ExitReason:             exitReason,
		LauncherKind:           plan.LauncherKind,
		RuntimeType:            plan.RuntimeType,
		RuntimeImage:           plan.Image,
		RuntimeMountTarget:     plan.BundleMountTarget,
		RuntimeBinDir:          agent.AgentBundleBinDir(plan.BundleMountTarget),
		RuntimeProfile:         plan.Profile,
		ExitCode:               &exitCode,
		Stdout:                 execResult.Stdout,
		Stderr:                 execResult.Stderr,
		StartedAt:              &startedAt,
		FinishedAt:             &finishedAt,
		DurationMS:             finishedAt.Sub(startedAt).Milliseconds(),
		ManagedProxyReportJSON: managedProxyReportJSON(execResult.ManagedProxyReport),
	}
	return result, nil
}

func (h *Harness) launchPlan(request agent.Request) agent.LaunchPlan {
	var runtimeType domain.AgentRuntimeType
	if request.Agent.Runtime != nil {
		runtimeType = request.Agent.Runtime.Type
	}
	plan := agent.LaunchPlan{
		LauncherKind:   h.launcherForRuntime(runtimeType).Kind(),
		Command:        h.command(request.Agent),
		CWD:            h.cwd(request),
		User:           h.user(request),
		Timeout:        h.timeout(request),
		Profile:        h.profileName(request.Agent),
		MaxTurns:       h.maxTurns(request.Agent),
		OutputFormat:   h.outputFormat(request.Agent),
		AllowedTools:   h.allowedTools(request.Agent),
		IdleTimeoutSec: h.idleTimeoutSec(request),
		ManagedProxy:   request.ManagedProxy,
	}
	if runtime := request.Agent.Runtime; runtime != nil {
		plan.RuntimeType = runtime.Type
		plan.Image = runtime.Image
		if runtime.Type == domain.AgentRuntimeTypeAgentImage {
			plan.BundleMountTarget = agent.AgentBundleMountTargetForSpec(request.Agent)
		}
		if runtime.Session != nil {
			plan.SessionMode = runtime.Session.Mode
			plan.SessionID = runtime.Session.SessionID
		}
	}
	plan.Env = h.env(request, plan)
	return plan
}

func (h *Harness) launcherForRuntime(runtimeType domain.AgentRuntimeType) agent.Launcher {
	if h.Launcher != nil {
		return h.Launcher
	}
	if runtimeType == domain.AgentRuntimeTypeAgentImage {
		return agent.MountedBundleLauncher{}
	}
	return agent.SandboxCommandLauncher{}
}

func (h *Harness) command(agentSpec domain.AgentSpec) sandbox.ExecCommand {
	return sandbox.ShellCommand(h.defaultCommand(agentSpec))
}

func (h *Harness) defaultCommand(agentSpec domain.AgentSpec) string {
	maxTurns := h.maxTurns(agentSpec)
	outputFormat := h.outputFormat(agentSpec)
	allowedTools := h.allowedTools(agentSpec)
	if maxTurns == 0 && outputFormat == "" && len(allowedTools) == 0 {
		return withApprovalPolicy(defaultCommand, agentSpec.ApprovalPolicy)
	}
	var builder strings.Builder
	builder.WriteString(`claude -p --model "$AXRUN_MODEL_ID"`)
	switch agentSpec.ApprovalPolicy {
	case domain.AgentApprovalPolicyNever:
		builder.WriteString(" --permission-mode bypassPermissions")
	case domain.AgentApprovalPolicyOnRequest:
		builder.WriteString(" --permission-mode default")
	}
	if outputFormat != "" {
		builder.WriteString(" --output-format ")
		builder.WriteString(shellQuote(outputFormat))
	}
	if maxTurns > 0 {
		builder.WriteString(fmt.Sprintf(" --max-turns %d", maxTurns))
	}
	if len(allowedTools) > 0 {
		builder.WriteString(" --tools ")
		builder.WriteString(shellQuote(strings.Join(allowedTools, ",")))
	}
	builder.WriteString(` "$AXRUN_TASK_INSTRUCTION"`)
	return builder.String()
}

func withApprovalPolicy(command string, policy domain.AgentApprovalPolicy) string {
	mode := ""
	switch policy {
	case domain.AgentApprovalPolicyNever:
		mode = "bypassPermissions"
	case domain.AgentApprovalPolicyOnRequest:
		mode = "default"
	}
	if mode == "" {
		return command
	}
	return strings.Replace(command, `claude -p`, `claude -p --permission-mode `+mode, 1)
}

func (h *Harness) maxTurns(agentSpec domain.AgentSpec) int {
	if h.Config.MaxTurns > 0 {
		return h.Config.MaxTurns
	}
	if runtime := agentSpec.Runtime; runtime != nil {
		return runtime.MaxTurns
	}
	return 0
}

func (h *Harness) outputFormat(agentSpec domain.AgentSpec) string {
	if h.Config.OutputFormat != "" {
		return h.Config.OutputFormat
	}
	if runtime := agentSpec.Runtime; runtime != nil {
		return runtime.OutputFormat
	}
	return ""
}

func (h *Harness) allowedTools(agentSpec domain.AgentSpec) []string {
	if len(h.Config.AllowedTools) > 0 {
		return append([]string(nil), h.Config.AllowedTools...)
	}
	if runtime := agentSpec.Runtime; runtime != nil {
		return append([]string(nil), runtime.AllowedTools...)
	}
	return nil
}

func (h *Harness) cwd(request agent.Request) string {
	if h.Config.CWD != "" {
		return h.Config.CWD
	}
	if runtime := request.Agent.Runtime; runtime != nil && runtime.Workdir != "" {
		return runtime.Workdir
	}
	if request.Task.InitialState != nil && request.Task.InitialState.Workdir != "" {
		return request.Task.InitialState.Workdir
	}
	if request.Task.InitialState != nil && request.Task.Sandbox.Workdir != "" {
		return request.Task.Sandbox.Workdir
	}
	if request.Task.InitialState != nil && request.Episode.Sandbox.Workdir != "" {
		return request.Episode.Sandbox.Workdir
	}
	return defaultCWD
}

func (h *Harness) user(request agent.Request) string {
	if h.Config.User != "" {
		return h.Config.User
	}
	if runtime := request.Agent.Runtime; runtime != nil && runtime.User != "" {
		return runtime.User
	}
	if runtime := request.Agent.Runtime; runtime != nil && runtime.Type == domain.AgentRuntimeTypeAgentImage {
		return defaultBundleUser
	}
	return ""
}

func (h *Harness) timeout(request agent.Request) time.Duration {
	timeoutSec := h.Config.TimeoutSec
	if timeoutSec == 0 && request.Agent.Runtime != nil {
		timeoutSec = request.Agent.Runtime.TimeoutSec
	}
	if timeoutSec == 0 && request.Task.Timeouts != nil {
		timeoutSec = request.Task.Timeouts.AgentSec
	}
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSec
	}
	return time.Duration(timeoutSec) * time.Second
}

func (h *Harness) idleTimeoutSec(request agent.Request) int {
	if h.Config.IdleTimeoutSec > 0 {
		return h.Config.IdleTimeoutSec
	}
	if request.Agent.Runtime != nil {
		return request.Agent.Runtime.IdleTimeoutSec
	}
	return 0
}

func (h *Harness) env(request agent.Request, plan agent.LaunchPlan) map[string]string {
	env := map[string]string{}
	if runtime := request.Agent.Runtime; runtime != nil {
		for key, value := range runtime.Env {
			env[key] = value
		}
	}
	for key, value := range h.Config.Env {
		env[key] = value
	}
	if plan.User == defaultBundleUser {
		if _, configured := env["HOME"]; !configured {
			env["HOME"] = defaultBundleHome
		}
	}
	env["AXRUN_AGENT_NAME"] = request.Agent.Name
	env["AXRUN_AGENT_PROFILE"] = plan.Profile
	env["AXRUN_MODEL_ID"] = request.Model.ID
	env["AXRUN_TASK_ID"] = request.Task.ID
	env["AXRUN_TASK_INSTRUCTION"] = request.Instruction
	if plan.RuntimeType != "" {
		env["AXRUN_AGENT_RUNTIME_TYPE"] = string(plan.RuntimeType)
	}
	if plan.Image != "" {
		env["AXRUN_AGENT_RUNTIME_IMAGE"] = plan.Image
	}
	if plan.BundleMountTarget != "" {
		env["AXRUN_AGENT_BUNDLE_MOUNT_TARGET"] = plan.BundleMountTarget
	}
	if plan.SessionMode != "" {
		env["AXRUN_AGENT_SESSION_MODE"] = string(plan.SessionMode)
	}
	if plan.SessionID != "" {
		env["AXRUN_AGENT_SESSION_ID"] = plan.SessionID
	}
	if plan.MaxTurns > 0 {
		env["AXRUN_AGENT_MAX_TURNS"] = fmt.Sprintf("%d", plan.MaxTurns)
	}
	if plan.OutputFormat != "" {
		env["AXRUN_AGENT_OUTPUT_FORMAT"] = plan.OutputFormat
	}
	if len(plan.AllowedTools) > 0 {
		env["AXRUN_AGENT_ALLOWED_TOOLS"] = strings.Join(plan.AllowedTools, ",")
	}
	if plan.IdleTimeoutSec > 0 {
		env["AXRUN_AGENT_IDLE_TIMEOUT_SEC"] = fmt.Sprintf("%d", plan.IdleTimeoutSec)
	}
	env["ANTHROPIC_MODEL"] = request.Model.ID
	if plan.Profile != "" {
		env["ANTHROPIC_API_KEY"] = "axern-local-adapter"
	} else if _, ok := env["ANTHROPIC_API_KEY"]; !ok {
		env["ANTHROPIC_API_KEY"] = "axern-axrun-placeholder"
	}
	return env
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (h *Harness) profileName(spec domain.AgentSpec) string {
	if runtime := spec.Runtime; runtime != nil && runtime.Profile != "" {
		return strings.TrimSpace(runtime.Profile)
	}
	return strings.TrimSpace(spec.Profile)
}

func managedProxyReportJSON(report *sandbox.ManagedProxyReport) []byte {
	if report == nil {
		return nil
	}
	return append([]byte(nil), report.ReportJSON...)
}
