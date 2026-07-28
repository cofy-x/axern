package codex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

const (
	defaultCommandPrefix = `answer_path="${AXRUN_CODEX_OUTPUT_LAST_MESSAGE:-/tmp/axrun-codex-last-message.txt}"; codex exec --color never --model "$AXRUN_MODEL_ID" --cd "$AXRUN_AGENT_CWD"`
	defaultCommandSuffix = ` --skip-git-repo-check --output-last-message "$answer_path" "$AXRUN_TASK_INSTRUCTION" && cat "$answer_path"`
	defaultCWD           = "/workspace"
	defaultTimeoutSec    = 1800
)

type Harness struct {
	Config   Config
	Launcher agent.Launcher
}

func New() *Harness {
	return NewWithConfig(Config{})
}

func NewWithConfig(config Config) *Harness {
	return &Harness{Config: config}
}

func NewFromEnv() (*Harness, error) {
	config, err := ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewWithConfig(config), nil
}

func Command(policy domain.AgentApprovalPolicy) string {
	command := defaultCommandPrefix
	switch policy {
	case domain.AgentApprovalPolicyNever:
		command += " --dangerously-bypass-approvals-and-sandbox"
	case domain.AgentApprovalPolicyOnRequest:
		command += ` --sandbox workspace-write -c 'approval_policy="on-request"'`
	}
	return command + defaultCommandSuffix
}

func (h *Harness) SetLauncher(launcher agent.Launcher) {
	h.Launcher = launcher
}

func (h *Harness) Preflight() error {
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
	summary := "codex agent completed"
	errorText := ""
	exitReason := domain.AgentExitReasonCompleted
	if exitCode != 0 {
		status = domain.AgentStatusFailed
		summary = fmt.Sprintf("codex agent exited with status %d", exitCode)
		errorText = summary
		exitReason = domain.AgentExitReasonCommandNonzero
	}
	return agent.Result{
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
	}, nil
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
	return sandbox.ShellCommand(Command(agentSpec.ApprovalPolicy))
}

func (h *Harness) cwd(request agent.Request) string {
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
	if request.Episode.Sandbox.Workdir != "" {
		return request.Episode.Sandbox.Workdir
	}
	if request.Task.Sandbox.Workdir != "" {
		return request.Task.Sandbox.Workdir
	}
	return defaultCWD
}

func (h *Harness) user(request agent.Request) string {
	if runtime := request.Agent.Runtime; runtime != nil && runtime.User != "" {
		return runtime.User
	}
	return ""
}

func (h *Harness) timeout(request agent.Request) time.Duration {
	timeoutSec := 0
	if request.Agent.Runtime != nil {
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

func (h *Harness) maxTurns(agentSpec domain.AgentSpec) int {
	if runtime := agentSpec.Runtime; runtime != nil {
		return runtime.MaxTurns
	}
	return 0
}

func (h *Harness) outputFormat(agentSpec domain.AgentSpec) string {
	if runtime := agentSpec.Runtime; runtime != nil {
		return runtime.OutputFormat
	}
	return ""
}

func (h *Harness) allowedTools(agentSpec domain.AgentSpec) []string {
	if runtime := agentSpec.Runtime; runtime != nil {
		return append([]string(nil), runtime.AllowedTools...)
	}
	return nil
}

func (h *Harness) idleTimeoutSec(request agent.Request) int {
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
	env["AXRUN_AGENT_NAME"] = request.Agent.Name
	env["AXRUN_AGENT_PROFILE"] = plan.Profile
	env["AXRUN_MODEL_ID"] = request.Model.ID
	env["AXRUN_TASK_ID"] = request.Task.ID
	env["AXRUN_TASK_INSTRUCTION"] = request.Instruction
	env["AXRUN_AGENT_CWD"] = plan.CWD
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
	return env
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

type commandRecorder struct {
	recorder *proxy.Recorder
}

func newCommandRecorder(recorder *proxy.Recorder) *commandRecorder {
	if recorder == nil {
		return nil
	}
	return &commandRecorder{recorder: recorder}
}

func (r *commandRecorder) recordCommandStarted(plan agent.LaunchPlan) {
	if r == nil {
		return
	}
	timestamp := time.Now().UTC()
	commandVector, commandText := commandFields(plan.Command)
	r.recorder.AppendEvent(domain.AgentRawEvent{
		Type:               domain.AgentRawEventCommandStarted,
		Timestamp:          &timestamp,
		LauncherKind:       plan.LauncherKind,
		RuntimeType:        plan.RuntimeType,
		RuntimeImage:       plan.Image,
		RuntimeMountTarget: plan.BundleMountTarget,
		RuntimeBinDir:      agent.AgentBundleBinDir(plan.BundleMountTarget),
		RuntimeProfile:     plan.Profile,
		Command:            commandVector,
		CommandText:        commandText,
		CWD:                plan.CWD,
		User:               plan.User,
		TimeoutSec:         int(plan.Timeout.Seconds()),
	})
}

func (r *commandRecorder) recordCommandFinished(plan agent.LaunchPlan, startedAt time.Time, exitCode *int, err error) {
	if r == nil {
		return
	}
	timestamp := time.Now().UTC()
	commandVector, commandText := commandFields(plan.Command)
	event := domain.AgentRawEvent{
		Type:               domain.AgentRawEventCommandFinished,
		Timestamp:          &timestamp,
		LauncherKind:       plan.LauncherKind,
		RuntimeType:        plan.RuntimeType,
		RuntimeImage:       plan.Image,
		RuntimeMountTarget: plan.BundleMountTarget,
		RuntimeBinDir:      agent.AgentBundleBinDir(plan.BundleMountTarget),
		RuntimeProfile:     plan.Profile,
		Command:            commandVector,
		CommandText:        commandText,
		CWD:                plan.CWD,
		User:               plan.User,
		TimeoutSec:         int(plan.Timeout.Seconds()),
		LatencyMS:          time.Since(startedAt).Milliseconds(),
		ExitCode:           exitCode,
	}
	if err != nil {
		event.Error = err.Error()
	}
	r.recorder.AppendEvent(event)
}

func commandFields(command sandbox.ExecCommand) ([]string, string) {
	if command.Shell() != "" {
		return nil, command.Shell()
	}
	return command.Argv(), ""
}
