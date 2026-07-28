package codex

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
	"github.com/cofy-x/axern/lib/go/agentprofile"
)

func TestHarnessBuildsDefaultCodexCommandAndEnv(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{Stdout: "done"}}
	result, err := New().Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name:           "codex",
			ApprovalPolicy: domain.AgentApprovalPolicyNever,
			Runtime: &domain.AgentRuntimeSpec{
				Type:       domain.AgentRuntimeTypeSandboxCommand,
				Workdir:    "/workspace",
				TimeoutSec: 7,
				Env:        map[string]string{"CUSTOM": "1"},
			},
		},
		Model:       domain.ModelSpec{ID: "gpt-5.4"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Edit the file",
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
	command := sb.command.Shell()
	for _, expected := range []string{
		"codex exec",
		`--model "$AXRUN_MODEL_ID"`,
		`--cd "$AXRUN_AGENT_CWD"`,
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		`--output-last-message "$answer_path"`,
		`"$AXRUN_TASK_INSTRUCTION"`,
		`cat "$answer_path"`,
	} {
		if !strings.Contains(command, expected) {
			t.Fatalf("command %q missing %q", command, expected)
		}
	}
	if sb.options.CWD != "/workspace" || sb.options.User != "" || sb.options.Timeout != 7*time.Second {
		t.Fatalf("exec options = %#v", sb.options)
	}
	if sb.options.Env["AXRUN_TASK_INSTRUCTION"] != "Edit the file" ||
		sb.options.Env["AXRUN_MODEL_ID"] != "gpt-5.4" ||
		sb.options.Env["AXRUN_AGENT_CWD"] != "/workspace" ||
		sb.options.Env["CUSTOM"] != "1" {
		t.Fatalf("env = %#v", sb.options.Env)
	}
	if _, ok := sb.options.Env["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("codex env must not inject ANTHROPIC_API_KEY: %#v", sb.options.Env)
	}
}

func TestCommandOnRequestEnforcesApprovalAndSandboxPolicy(t *testing.T) {
	command := Command(domain.AgentApprovalPolicyOnRequest)
	if strings.Contains(command, "dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("on_request command bypasses approvals: %q", command)
	}
	for _, expected := range []string{`--sandbox workspace-write`, `approval_policy="on-request"`} {
		if !strings.Contains(command, expected) {
			t.Fatalf("on_request command %q missing %q", command, expected)
		}
	}
}

func TestHarnessBuildsManagedCommandForAgentImageProfile(t *testing.T) {
	launcher := &recordingLauncher{
		kind:   domain.AgentLauncherKindAgentImage,
		result: sandbox.ExecResult{Stdout: "done"},
	}
	h := New()
	h.Launcher = launcher
	result, err := h.Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name:           "codex",
			ApprovalPolicy: domain.AgentApprovalPolicyNever,
			Runtime: &domain.AgentRuntimeSpec{
				Type:           domain.AgentRuntimeTypeAgentImage,
				Image:          "axern/codex-bundle:dev",
				Profile:        "codex-smoke",
				Workdir:        "/workspace",
				User:           "axern",
				TimeoutSec:     9,
				MaxTurns:       42,
				OutputFormat:   "json",
				AllowedTools:   []string{"shell"},
				IdleTimeoutSec: 30,
				Env:            map[string]string{"RUNTIME_ENV": "yes"},
				Session:        &domain.AgentSessionSpec{Mode: domain.AgentSessionModeCreate, SessionID: "session-1"},
			},
		},
		Model:       domain.ModelSpec{ID: "gpt-5.4"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     &fakeSandbox{},
		Instruction: "Do it",
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.LauncherKind != domain.AgentLauncherKindAgentImage ||
		result.RuntimeType != domain.AgentRuntimeTypeAgentImage ||
		result.RuntimeImage != "axern/codex-bundle:dev" ||
		result.RuntimeProfile != "codex-smoke" {
		t.Fatalf("result = %#v", result)
	}
	if launcher.plan.Image != "axern/codex-bundle:dev" ||
		launcher.plan.BundleMountTarget != "/opt/axern/agents/codex" ||
		launcher.plan.Profile != "codex-smoke" ||
		launcher.plan.CWD != "/workspace" ||
		launcher.plan.User != "axern" ||
		launcher.plan.Timeout != 9*time.Second {
		t.Fatalf("plan = %#v", launcher.plan)
	}
	command := launcher.plan.Command.Shell()
	if !strings.Contains(command, "codex exec") || strings.Contains(command, "printf ok") {
		t.Fatalf("command = %#v", launcher.plan.Command)
	}
	if launcher.plan.Env["AXRUN_AGENT_RUNTIME_IMAGE"] != "axern/codex-bundle:dev" ||
		launcher.plan.Env["AXRUN_AGENT_BUNDLE_MOUNT_TARGET"] != "/opt/axern/agents/codex" ||
		launcher.plan.Env["AXRUN_AGENT_PROFILE"] != "codex-smoke" ||
		launcher.plan.Env["AXRUN_AGENT_SESSION_ID"] != "session-1" ||
		launcher.plan.Env["AXRUN_AGENT_MAX_TURNS"] != "42" ||
		launcher.plan.Env["AXRUN_AGENT_OUTPUT_FORMAT"] != "json" ||
		launcher.plan.Env["AXRUN_AGENT_ALLOWED_TOOLS"] != "shell" ||
		launcher.plan.Env["RUNTIME_ENV"] != "yes" {
		t.Fatalf("env = %#v", launcher.plan.Env)
	}
	if _, ok := launcher.plan.Env["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("codex env must not inject ANTHROPIC_API_KEY: %#v", launcher.plan.Env)
	}
}

func TestHarnessManagedProxyConfigReturnsSetupFromGenericProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "agent_profiles": {
    "profiles": {
      "codex-smoke": {
        "agent": "codex",
        "provider": "openai",
        "wire_api": "responses",
        "upstream": "https://api.example.test/v1",
        "token": "sk-test",
        "env": {"OPENAI_ORGANIZATION": "org-test", "OPENAI_PROJECT": "project-test"},
        "config": {}
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	setup, err := NewWithConfig(Config{ConfigPath: configPath}).ManagedProxyConfig(domain.AgentSpec{Name: "codex", Profile: "codex-smoke"})
	if err != nil {
		t.Fatalf("ManagedProxyConfig returned error: %v", err)
	}
	if setup == nil ||
		setup.Upstream.String() != "https://api.example.test/v1" ||
		setup.Token != "sk-test" ||
		setup.ProviderType != agent.ProviderOpenAI {
		t.Fatalf("setup = %#v", setup)
	}
}

func TestHarnessRunWritesRemoteCodexConfigWithManagedProxyConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "agent_profiles": {
    "profiles": {
      "codex-smoke": {
        "agent": "codex",
        "provider": "openai",
        "wire_api": "responses",
        "upstream": "https://api.example.test/v1",
        "token": "sk-test",
        "env": {"OPENAI_ORGANIZATION": "org-test", "OPENAI_PROJECT": "project-test"},
        "config": {}
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
	h := NewWithConfig(Config{ConfigPath: configPath})
	h.Launcher = launcher
	if _, err := h.ManagedProxyConfig(domain.AgentSpec{Name: "codex", Profile: "codex-smoke"}); err != nil {
		t.Fatalf("ManagedProxyConfig returned error: %v", err)
	}
	result, err := h.Run(context.Background(), agent.Request{
		Agent: domain.AgentSpec{
			Name: "codex",
			Runtime: &domain.AgentRuntimeSpec{
				Type:    domain.AgentRuntimeTypeAgentImage,
				Image:   "axern/codex-bundle:dev",
				Profile: "codex-smoke",
				Command: []string{"bash", "-lc", "printf ok"},
				User:    "axern",
				Workdir: "/workspace",
				Env:     map[string]string{"OPENAI_ORGANIZATION": "runtime-org"},
			},
		},
		Model:       domain.ModelSpec{ID: "gpt-5.4"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     &fakeSandbox{},
		Instruction: "Do it",
		ManagedProxy: &sandbox.ManagedProxyOptions{
			Provider:            "openai",
			UpstreamBaseURL:     "https://api.example.test/v1",
			UpstreamBearerToken: "sk-test",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("result = %#v", result)
	}
	configScript := launcher.plan.Command.Shell()
	for _, expected := range []string{"model_providers.axern", `base_url = "${AXERN_MANAGED_PROXY_BASE_URL}"`, `wire_api = "responses"`, `OPENAI_API_KEY="${AXERN_MANAGED_PROXY_TOKEN}"`} {
		if !strings.Contains(configScript, expected) {
			t.Fatalf("config script missing %q: %s", expected, configScript)
		}
	}
	if !strings.Contains(configScript, "codex exec") || strings.Contains(configScript, "printf ok") {
		t.Fatalf("config script missing managed Codex command: %s", configScript)
	}
	if launcher.plan.ManagedProxy == nil || launcher.plan.ManagedProxy.Provider != "openai" {
		t.Fatalf("managed proxy = %#v", launcher.plan.ManagedProxy)
	}
	if got := launcher.plan.Env["OPENAI_ORGANIZATION"]; got != "runtime-org" {
		t.Fatalf("OPENAI_ORGANIZATION = %q", got)
	}
	if got := launcher.plan.Env["OPENAI_PROJECT"]; got != "project-test" {
		t.Fatalf("OPENAI_PROJECT = %q", got)
	}
	if _, ok := launcher.plan.Env["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("codex env must not inject ANTHROPIC_API_KEY: %#v", launcher.plan.Env)
	}
}

func TestCodexConfigTOMLQuotesProviderKeyWhenNeeded(t *testing.T) {
	config, err := codexConfigTOML(agent.Profile{
		WireAPI: agentprofile.WireAPIResponses,
		Config: map[string]string{
			"codex_provider": "openai.compat",
		},
	})
	if err != nil {
		t.Fatalf("codexConfigTOML returned error: %v", err)
	}
	if !strings.Contains(config, `[model_providers."openai.compat"]`) {
		t.Fatalf("config = %s", config)
	}
}

func TestCodexConfigTOMLRejectsUnsupportedWireAPI(t *testing.T) {
	_, err := codexConfigTOML(agent.Profile{
		WireAPI: "chat",
	})
	if err == nil || !strings.Contains(err.Error(), "wire_api") {
		t.Fatalf("err = %v", err)
	}
}

func TestHarnessNonzeroReturnsAgentFailureWithoutError(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 2, Stderr: "bad"}}
	result, err := New().Run(context.Background(), agent.Request{
		Agent:       domain.AgentSpec{Name: "codex", Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeSandboxCommand}},
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

func TestHarnessDefaultsToWorkspaceCWD(t *testing.T) {
	sb := &fakeSandbox{}
	_, err := New().Run(context.Background(), agent.Request{
		Agent:       domain.AgentSpec{Name: "codex", Runtime: &domain.AgentRuntimeSpec{Type: domain.AgentRuntimeTypeSandboxCommand}},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
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
