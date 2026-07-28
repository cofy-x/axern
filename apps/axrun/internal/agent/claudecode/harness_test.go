package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestConfigFromEnvParsesAgentSettings(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_CWD", "/workspace")
	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "42")
	t.Setenv("AXRUN_CLAUDE_CODE_MAX_TURNS", "200")
	t.Setenv("AXRUN_CLAUDE_CODE_OUTPUT_FORMAT", "json")
	t.Setenv("AXRUN_CLAUDE_CODE_ALLOWED_TOOLS", "Bash,Edit")
	t.Setenv("AXRUN_CLAUDE_CODE_IDLE_TIMEOUT_SEC", "600")
	t.Setenv("AXRUN_CLAUDE_CODE_ENV", "A=1,B=two")

	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv returned error: %v", err)
	}
	if config.CWD != "/workspace" ||
		config.TimeoutSec != 42 ||
		config.MaxTurns != 200 ||
		config.OutputFormat != "json" ||
		len(config.AllowedTools) != 2 ||
		config.AllowedTools[1] != "Edit" ||
		config.IdleTimeoutSec != 600 ||
		config.Env["B"] != "two" {
		t.Fatalf("config = %#v", config)
	}
}

func TestConfigFromEnvRejectsInvalidTimeoutAndEnv(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "nope")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv timeout error = nil")
	}

	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "")
	t.Setenv("AXRUN_CLAUDE_CODE_ENV", "missing-equals")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv env error = nil")
	}
}

func TestConfigFromEnvRejectsInvalidRuntimeNumbers(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_MAX_TURNS", "-1")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv max turns error = nil")
	}

	t.Setenv("AXRUN_CLAUDE_CODE_MAX_TURNS", "")
	t.Setenv("AXRUN_CLAUDE_CODE_IDLE_TIMEOUT_SEC", "-1")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("ConfigFromEnv idle timeout error = nil")
	}
}

func TestHarnessRunExecutesManagedCommandWithInstructionEnv(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{Stdout: "done"}}
	harness := New(Config{CWD: "/work", TimeoutSec: 7, Env: map[string]string{"CUSTOM": "1"}})

	result, err := harness.Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name:           "claude-code",
			ApprovalPolicy: domain.AgentApprovalPolicyNever,
		},
		Model:       domain.ModelSpec{ID: "anthropic/claude-haiku-4-5"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Episode:     domain.Episode{},
		Sandbox:     sb,
		Instruction: "Print hello",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted ||
		result.ExitReason != domain.AgentExitReasonCompleted ||
		result.Stdout != "done" ||
		result.ExitCode == nil ||
		*result.ExitCode != 0 {
		t.Fatalf("result = %#v", result)
	}
	if !strings.Contains(sb.command.Shell(), "claude -p") ||
		!strings.Contains(sb.command.Shell(), "--permission-mode bypassPermissions") ||
		sb.options.CWD != "/work" || sb.options.User != "" || sb.options.Timeout != 7*time.Second {
		t.Fatalf("exec = %#v %#v", sb.command, sb.options)
	}
	if sb.options.Env["AXRUN_TASK_INSTRUCTION"] != "Print hello" ||
		sb.options.Env["AXRUN_MODEL_ID"] != "anthropic/claude-haiku-4-5" ||
		sb.options.Env["CUSTOM"] != "1" ||
		sb.options.Env["ANTHROPIC_API_KEY"] == "" {
		t.Fatalf("env = %#v", sb.options.Env)
	}
}

func TestHarnessOnRequestEnforcesDefaultPermissionMode(t *testing.T) {
	command := New(Config{}).defaultCommand(domain.AgentSpec{ApprovalPolicy: domain.AgentApprovalPolicyOnRequest})
	if strings.Contains(command, "bypassPermissions") {
		t.Fatalf("on_request command bypasses permissions: %q", command)
	}
	if !strings.Contains(command, "--permission-mode default") {
		t.Fatalf("on_request command does not enforce default permission mode: %q", command)
	}
}

func TestHarnessRunWritesRemoteConfigWithManagedProxyConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "agent_profiles": {
    "profiles": {
      "deepseek": {
        "agent": "claude-code",
        "provider": "anthropic",
        "wire_api": "anthropic_messages",
        "upstream": "https://api.deepseek.com/anthropic",
        "token": "sk-test",
        "config": {"sonnet_model": "deepseek-v4-flash"}
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	sb := &fakeSandbox{}
	result, err := New(Config{ConfigPath: configPath}).Run(context.Background(), agent.Request{
		Agent:       domain.AgentSpec{Name: "claude-code", Profile: "deepseek"},
		Model:       domain.ModelSpec{ID: "deepseek-v4-flash"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Do it",
		ManagedProxy: &sandbox.ManagedProxyOptions{
			Provider:            "anthropic",
			UpstreamBaseURL:     "https://api.deepseek.com/anthropic",
			UpstreamBearerToken: "sk-test",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if len(sb.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(sb.commands))
	}
	configScript := sb.commands[0].Shell()
	if !strings.Contains(configScript, "ANTHROPIC_BASE_URL") || !strings.Contains(configScript, "${AXERN_MANAGED_PROXY_BASE_URL}") {
		t.Fatalf("config command = %#v", sb.commands[0])
	}
	if !strings.Contains(configScript, "claude -p") {
		t.Fatalf("agent command missing: %#v", sb.commands[0])
	}
	if sb.options.Env["ANTHROPIC_API_KEY"] != "axern-local-adapter" {
		t.Fatalf("env = %#v", sb.options.Env)
	}
	if sb.options.User != "" {
		t.Fatalf("user = %q, want sandbox image default", sb.options.User)
	}
}

func TestHarnessProfileDoesNotInferSandboxUser(t *testing.T) {
	plan := New(Config{}).launchPlan(agent.Request{
		Agent: domain.AgentSpec{
			Name:    "claude-code",
			Profile: "production",
			Runtime: &domain.AgentRuntimeSpec{
				Type:  domain.AgentRuntimeTypeAgentImage,
				Image: "registry.example.com/claude-code@sha256:bundle",
			},
		},
		Task: domain.TaskInstance{ID: "task-1"},
	})

	if plan.User != defaultBundleUser {
		t.Fatalf("user = %q, want portable non-root bundle user", plan.User)
	}
	if plan.Env["HOME"] != defaultBundleHome {
		t.Fatalf("HOME = %q, want %q", plan.Env["HOME"], defaultBundleHome)
	}
}

func TestHarnessPreservesExplicitSandboxUser(t *testing.T) {
	plan := New(Config{}).launchPlan(agent.Request{
		Agent: domain.AgentSpec{
			Name:    "claude-code",
			Profile: "production",
			Runtime: &domain.AgentRuntimeSpec{
				Type:  domain.AgentRuntimeTypeAgentImage,
				Image: "registry.example.com/claude-code@sha256:bundle",
				User:  "runner",
			},
		},
		Task: domain.TaskInstance{ID: "task-1"},
	})

	if plan.User != "runner" {
		t.Fatalf("user = %q, want runner", plan.User)
	}
}

func TestHarnessPreservesExplicitBundleHome(t *testing.T) {
	plan := New(Config{Env: map[string]string{"HOME": "/custom-home"}}).launchPlan(agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				Type:  domain.AgentRuntimeTypeAgentImage,
				Image: "registry.example.com/claude-code@sha256:bundle",
			},
		},
		Task: domain.TaskInstance{ID: "task-1"},
	})

	if plan.Env["HOME"] != "/custom-home" {
		t.Fatalf("HOME = %q, want explicit value", plan.Env["HOME"])
	}
}

func TestHarnessRunWrapsAgentImageCommandWithRemoteConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "agent_profiles": {
    "profiles": {
      "deepseek": {
        "agent": "claude-code",
        "provider": "anthropic",
        "wire_api": "anthropic_messages",
        "upstream": "https://api.deepseek.com/anthropic",
        "token": "sk-test",
        "config": {"sonnet_model": "deepseek-v4-flash"}
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	launcher := &recordingLauncher{
		kind:   domain.AgentLauncherKindAgentImage,
		result: sandbox.ExecResult{Stdout: "done"},
	}
	h := New(Config{ConfigPath: configPath})
	h.Launcher = launcher
	if _, err := h.ManagedProxyConfig(domain.AgentSpec{Name: "claude-code", Profile: "deepseek"}); err != nil {
		t.Fatalf("ManagedProxyConfig returned error: %v", err)
	}
	result, err := h.Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				Type:    domain.AgentRuntimeTypeAgentImage,
				Image:   "axern/claude-code-bundle:dev",
				Profile: "deepseek",
				Command: []string{"bash", "-lc", "printf ok"},
				User:    "axern",
				Workdir: "/workspace",
			},
		},
		Model:       domain.ModelSpec{ID: "deepseek-v4-flash"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     &fakeSandbox{},
		Instruction: "Do it",
		ManagedProxy: &sandbox.ManagedProxyOptions{
			Provider:            "anthropic",
			UpstreamBaseURL:     "https://api.deepseek.com/anthropic",
			UpstreamBearerToken: "sk-test",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	command := launcher.plan.Command.Shell()
	for _, expected := range []string{"ANTHROPIC_BASE_URL", "${AXERN_MANAGED_PROXY_BASE_URL}", "claude -p"} {
		if !strings.Contains(command, expected) {
			t.Fatalf("wrapped command missing %q: %s", expected, command)
		}
	}
}

func TestHarnessManagedProxyConfigReturnsSetupFromProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "agent_profiles": {
    "profiles": {
      "deepseek": {
        "agent": "claude-code",
        "provider": "anthropic",
        "wire_api": "anthropic_messages",
        "upstream": "https://api.deepseek.com/anthropic",
        "token": "sk-test"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	h := New(Config{ConfigPath: configPath})
	setup, err := h.ManagedProxyConfig(domain.AgentSpec{Name: "claude-code", Profile: "deepseek"})
	if err != nil {
		t.Fatalf("ManagedProxyConfig returned error: %v", err)
	}
	if setup == nil {
		t.Fatal("ManagedProxyConfig returned nil")
	}
	if setup.Upstream == nil || setup.Upstream.String() != "https://api.deepseek.com/anthropic" || setup.Token != "sk-test" || setup.ProviderType != agent.ProviderAnthropic {
		t.Fatalf("setup = %#v", setup)
	}
}

func TestHarnessManagedProxyConfigReturnsNilWithoutProfile(t *testing.T) {
	h := New(Config{})
	setup, err := h.ManagedProxyConfig(domain.AgentSpec{Name: "claude-code"})
	if err != nil {
		t.Fatalf("ManagedProxyConfig returned error: %v", err)
	}
	if setup != nil {
		t.Fatalf("ManagedProxyConfig should return nil without profile, got %#v", setup)
	}
}

func TestHarnessRunUsesAgentRuntimeExecutionSpec(t *testing.T) {
	launcher := &recordingLauncher{
		kind:   domain.AgentLauncherKindAgentImage,
		result: sandbox.ExecResult{Stdout: "done"},
	}
	h := New(Config{})
	h.Launcher = launcher
	result, err := h.Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				Type:           domain.AgentRuntimeTypeAgentImage,
				Image:          "axern/claude-code-bundle:dev",
				Command:        []string{"bash", "-lc", "printf ok"},
				Workdir:        "/workspace",
				User:           "axrun",
				TimeoutSec:     9,
				MaxTurns:       42,
				OutputFormat:   "json",
				AllowedTools:   []string{"Bash"},
				IdleTimeoutSec: 30,
				Env:            map[string]string{"RUNTIME_ENV": "yes"},
				Session:        &domain.AgentSessionSpec{Mode: domain.AgentSessionModeCreate, SessionID: "session-1"},
			},
		},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     &fakeSandbox{},
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if result.LauncherKind != domain.AgentLauncherKindAgentImage ||
		result.RuntimeType != domain.AgentRuntimeTypeAgentImage ||
		result.RuntimeImage != "axern/claude-code-bundle:dev" {
		t.Fatalf("result launcher metadata = %#v", result)
	}
	command := launcher.plan.Command.Shell()
	if !strings.Contains(command, "claude -p") || strings.Contains(command, "printf ok") {
		t.Fatalf("command = %#v", launcher.plan.Command)
	}
	if launcher.plan.CWD != "/workspace" || launcher.plan.User != "axrun" || launcher.plan.Timeout != 9*time.Second ||
		launcher.plan.BundleMountTarget != "/opt/axern/agents/claude-code" {
		t.Fatalf("plan = %#v", launcher.plan)
	}
	if launcher.plan.Env["RUNTIME_ENV"] != "yes" ||
		launcher.plan.Env["AXRUN_AGENT_RUNTIME_TYPE"] != string(domain.AgentRuntimeTypeAgentImage) ||
		launcher.plan.Env["AXRUN_AGENT_RUNTIME_IMAGE"] != "axern/claude-code-bundle:dev" ||
		launcher.plan.Env["AXRUN_AGENT_BUNDLE_MOUNT_TARGET"] != "/opt/axern/agents/claude-code" ||
		launcher.plan.Env["AXRUN_AGENT_SESSION_MODE"] != string(domain.AgentSessionModeCreate) ||
		launcher.plan.Env["AXRUN_AGENT_SESSION_ID"] != "session-1" ||
		launcher.plan.Env["AXRUN_AGENT_MAX_TURNS"] != "42" ||
		launcher.plan.Env["AXRUN_AGENT_OUTPUT_FORMAT"] != "json" ||
		launcher.plan.Env["AXRUN_AGENT_ALLOWED_TOOLS"] != "Bash" ||
		launcher.plan.Env["AXRUN_AGENT_IDLE_TIMEOUT_SEC"] != "30" {
		t.Fatalf("env = %#v", launcher.plan.Env)
	}
}

func TestHarnessLaunchPlanUsesRuntimeProfileForEnv(t *testing.T) {
	plan := New(Config{}).launchPlan(agent.Request{
		Agent: domain.AgentSpec{
			Name:    "claude-code",
			Profile: "top-level-profile",
			Runtime: &domain.AgentRuntimeSpec{
				Type:    domain.AgentRuntimeTypeAgentImage,
				Image:   "axern/claude-code-bundle:dev",
				Command: []string{"bash", "-lc", "true"},
				Profile: "runtime-profile",
			},
		},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Instruction: "Do it",
	})
	if plan.Profile != "runtime-profile" ||
		plan.LauncherKind != domain.AgentLauncherKindAgentImage ||
		plan.RuntimeType != domain.AgentRuntimeTypeAgentImage ||
		plan.Image != "axern/claude-code-bundle:dev" ||
		plan.Env["AXRUN_AGENT_PROFILE"] != "runtime-profile" ||
		plan.Env["ANTHROPIC_API_KEY"] != "axern-local-adapter" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestHarnessIgnoresRuntimeCommandOverride(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{Stdout: "done"}}
	result, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				Type:    domain.AgentRuntimeTypeSandboxCommand,
				Command: []string{"bash", "-lc", "printf runtime"},
			},
		},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	if command := sb.command.Shell(); !strings.Contains(command, "claude -p") || strings.Contains(command, "printf runtime") {
		t.Fatalf("command = %#v", sb.command)
	}
}

func TestHarnessRunNonzeroReturnsAgentFailureWithoutError(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 2, Stderr: "bad"}}
	result, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent:       domain.AgentSpec{Name: "claude-code"},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusFailed ||
		result.ExitReason != domain.AgentExitReasonCommandNonzero ||
		result.Error == "" ||
		result.Stderr != "bad" {
		t.Fatalf("result = %#v", result)
	}
}

func TestHarnessBuildsDefaultCommandWithRuntimeClaudeOptions(t *testing.T) {
	sb := &fakeSandbox{}
	_, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				MaxTurns:     5,
				OutputFormat: "json",
				AllowedTools: []string{"Bash", "Edit"},
			},
		},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	command := sb.command.Shell()
	if !strings.Contains(command, "--output-format 'json'") ||
		!strings.Contains(command, "--max-turns 5") ||
		!strings.Contains(command, "--tools 'Bash,Edit'") {
		t.Fatalf("command = %#v", sb.command)
	}
}

func TestHarnessSelectsSandboxCommandLauncherForSandboxCommandRuntime(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{Stdout: "done"}}
	result, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				Type:    domain.AgentRuntimeTypeSandboxCommand,
				Command: []string{"bash", "-lc", "true"},
			},
		},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.LauncherKind != domain.AgentLauncherKindSandboxCommand {
		t.Fatalf("launcher_kind = %q, want sandbox-command", result.LauncherKind)
	}
}

func TestHarnessBuildsAgentImageLaunchPlanForAgentImageRuntime(t *testing.T) {
	plan := New(Config{}).launchPlan(agent.Request{
		Agent: domain.AgentSpec{
			Name: "claude-code",
			Runtime: &domain.AgentRuntimeSpec{
				Type:    domain.AgentRuntimeTypeAgentImage,
				Image:   "ghcr.io/cofy-x/claude-code-bundle:latest",
				Command: []string{"bash", "-lc", "true"},
			},
		},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Instruction: "Do it",
	})
	if plan.LauncherKind != domain.AgentLauncherKindAgentImage {
		t.Fatalf("launcher_kind = %q, want agent-image", plan.LauncherKind)
	}
	if plan.RuntimeType != domain.AgentRuntimeTypeAgentImage {
		t.Fatalf("runtime_type = %q", plan.RuntimeType)
	}
	if plan.Image != "ghcr.io/cofy-x/claude-code-bundle:latest" {
		t.Fatalf("runtime_image = %q", plan.Image)
	}
	if plan.BundleMountTarget != "/opt/axern/agents/claude-code" {
		t.Fatalf("bundle_mount_target = %q", plan.BundleMountTarget)
	}
}

func TestHarnessDefaultsToClaudeCodeHomeWithoutTaskWorkspace(t *testing.T) {
	sb := &fakeSandbox{}
	_, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{Name: "claude-code"},
		Model: domain.ModelSpec{ID: "model"},
		Task: domain.TaskInstance{
			ID:      "task-1",
			Sandbox: domain.SandboxSpec{Workdir: "/workspace"},
		},
		Sandbox:     sb,
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if sb.options.CWD != "/home/axern" {
		t.Fatalf("cwd = %q", sb.options.CWD)
	}
}

func TestHarnessUsesTaskWorkspaceWhenInitialStateExists(t *testing.T) {
	sb := &fakeSandbox{}
	_, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{Name: "claude-code"},
		Model: domain.ModelSpec{ID: "model"},
		Task: domain.TaskInstance{
			ID:           "task-1",
			Sandbox:      domain.SandboxSpec{Workdir: "/workspace"},
			InitialState: &domain.InitialStateSpec{Type: "directory"},
		},
		Sandbox:     sb,
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if sb.options.CWD != "/workspace" {
		t.Fatalf("cwd = %q", sb.options.CWD)
	}
}

type fakeSandbox struct {
	result   sandbox.ExecResult
	command  sandbox.ExecCommand
	commands []sandbox.ExecCommand
	options  sandbox.ExecOptions
}

type recordingLauncher struct {
	kind   domain.AgentLauncherKind
	plan   agent.LaunchPlan
	result sandbox.ExecResult
	err    error
}

func (l *recordingLauncher) Kind() domain.AgentLauncherKind {
	return l.kind
}

func (l *recordingLauncher) Launch(_ context.Context, _ sandbox.Instance, plan agent.LaunchPlan) (sandbox.ExecResult, error) {
	l.plan = plan
	return l.result, l.err
}

func (f *fakeSandbox) Exec(_ context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	f.command = command
	f.commands = append(f.commands, command)
	f.options = options
	return f.result, nil
}

func (f *fakeSandbox) UploadDir(context.Context, string, string, sandbox.UploadDirOptions) error {
	return nil
}

func (f *fakeSandbox) DownloadPath(context.Context, string, string, sandbox.DownloadPathOptions) error {
	return nil
}

func (f *fakeSandbox) State() (sandbox.State, error) {
	return sandbox.State{}, nil
}

func (f *fakeSandbox) Close(context.Context) error {
	return nil
}
