package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestNoopHarnessCompletes(t *testing.T) {
	result, err := (NoopHarness{}).Run(context.Background(), Request{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestCommandHarnessExecutesExplicitRuntimeCommand(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0, Stdout: "ok"}}
	result, err := (CommandHarness{}).Run(context.Background(), Request{
		Agent: domain.AgentSpec{Name: "command", Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeSandboxCommand,
			Command: []string{"/bin/sh", "-lc", "printf ok"},
			Workdir: "/workspace",
		}},
		Sandbox: sb,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted || result.Stdout != "ok" {
		t.Fatalf("result = %#v", result)
	}
	if got := sb.command.Argv(); len(got) != 3 || got[2] != "printf ok" {
		t.Fatalf("command = %#v", got)
	}
}

func TestCommandHarnessRecordsNonzeroExit(t *testing.T) {
	result, err := (CommandHarness{}).Run(context.Background(), Request{
		Agent: domain.AgentSpec{Name: "command", Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeSandboxCommand,
			Command: []string{"false"},
		}},
		Sandbox: &fakeSandbox{result: sandbox.ExecResult{ExitCode: 7}},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusFailed || result.ExitCode == nil || *result.ExitCode != 7 {
		t.Fatalf("result = %#v", result)
	}
}

func TestOracleShellHarnessExecutesInSandbox(t *testing.T) {
	sb := &fakeSandbox{}
	result, err := (OracleShellHarness{Command: "solve", CWD: "/workspace", TimeoutSec: 7}).Run(context.Background(), Request{Sandbox: sb})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusCompleted {
		t.Fatalf("status = %q", result.Status)
	}
	if sb.command.Shell() != "solve" || sb.options.CWD != "/workspace" || sb.options.Timeout != 7*time.Second {
		t.Fatalf("exec = %#v %#v", sb.command, sb.options)
	}
}

func TestOracleShellHarnessNonzeroFailsAgent(t *testing.T) {
	result, err := (OracleShellHarness{Command: "solve"}).Run(context.Background(), Request{Sandbox: &fakeSandbox{result: sandbox.ExecResult{ExitCode: 2}}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Status != domain.AgentStatusFailed || result.Summary == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestLaunchPlanExecOptionsCloneEnv(t *testing.T) {
	plan := LaunchPlan{
		LauncherKind: domain.AgentLauncherKindSandboxCommand,
		Command:      sandbox.ShellCommand("true"),
		CWD:          "/workspace",
		User:         "axrun",
		Timeout:      3 * time.Second,
		Env:          map[string]string{"A": "1"},
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
	options := plan.ExecOptions()
	options.Env["A"] = "changed"
	if plan.Env["A"] != "1" {
		t.Fatalf("plan env mutated: %#v", plan.Env)
	}
	if options.CWD != "/workspace" || options.User != "axrun" || options.Timeout != 3*time.Second {
		t.Fatalf("options = %#v", options)
	}
}

func TestSandboxCommandLauncherExecutesLaunchPlan(t *testing.T) {
	sb := &fakeSandbox{result: sandbox.ExecResult{ExitCode: 0, Stdout: "ok"}}
	plan := LaunchPlan{
		LauncherKind: domain.AgentLauncherKindSandboxCommand,
		Command:      sandbox.ArgvCommand([]string{"agent", "run"}),
		CWD:          "/workspace",
		User:         "axern",
		Timeout:      5 * time.Second,
		Env:          map[string]string{"MODEL": "test"},
	}
	result, err := (SandboxCommandLauncher{}).Launch(context.Background(), sb, plan)
	if err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	if result.Stdout != "ok" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if got := sb.command.Argv(); len(got) != 2 || got[0] != "agent" || got[1] != "run" {
		t.Fatalf("command = %#v", got)
	}
	if sb.options.CWD != "/workspace" || sb.options.User != "axern" || sb.options.Timeout != 5*time.Second {
		t.Fatalf("options = %#v", sb.options)
	}
	sb.options.Env["MODEL"] = "mutated"
	if plan.Env["MODEL"] != "test" {
		t.Fatalf("plan env mutated: %#v", plan.Env)
	}
}

func TestMountedBundleLauncherExecutesMountedBundleInSandbox(t *testing.T) {
	sb := &fakeSandbox{results: []sandbox.ExecResult{
		{ExitCode: 0},
		{ExitCode: 0, Stdout: "bundle-ok"},
	}}
	plan := LaunchPlan{
		LauncherKind:      domain.AgentLauncherKindAgentImage,
		Image:             "ghcr.io/cofy-x/claude-code-bundle:latest",
		BundleMountTarget: "/opt/axern/agents/claude-code",
		Command:           sandbox.ArgvCommand([]string{"claude", "-p", "hello"}),
		CWD:               "/workspace",
		User:              "axern",
		Timeout:           5 * time.Second,
		Env:               map[string]string{"MODEL": "test", "PATH": "/usr/bin"},
	}
	result, err := (MountedBundleLauncher{}).Launch(context.Background(), sb, plan)
	if err != nil {
		t.Fatalf("Launch returned error: %v", err)
	}
	if result.Stdout != "bundle-ok" {
		t.Fatalf("stdout = %q", result.Stdout)
	}
	if len(sb.commands) != 2 {
		t.Fatalf("exec count = %d, want 2", len(sb.commands))
	}
	if !strings.Contains(sb.commands[0].Shell(), "test -d '/opt/axern/agents/claude-code'") ||
		!strings.Contains(sb.commands[0].Shell(), "test -d '/opt/axern/agents/claude-code/bin'") {
		t.Fatalf("self-check command = %#v", sb.commands[0])
	}
	if got := sb.commands[1].Shell(); !strings.Contains(got, "export PATH='/opt/axern/agents/claude-code/bin':\"${PATH:-") ||
		!strings.Contains(got, "exec 'claude' '-p' 'hello'") {
		t.Fatalf("command = %q", got)
	}
	if sb.options.CWD != "/workspace" || sb.options.User != "axern" || sb.options.Timeout != 5*time.Second {
		t.Fatalf("options = %#v", sb.options)
	}
	if got := sb.options.Env["PATH"]; got != "/usr/bin" {
		t.Fatalf("PATH = %q", got)
	}
	if got := sb.options.Env["AXRUN_AGENT_BUNDLE_MOUNT_TARGET"]; got != "/opt/axern/agents/claude-code" {
		t.Fatalf("mount target env = %q", got)
	}
}

func TestMountedBundleLauncherFailsSelfCheck(t *testing.T) {
	sb := &fakeSandbox{results: []sandbox.ExecResult{{ExitCode: 1}}}
	plan := LaunchPlan{
		LauncherKind:      domain.AgentLauncherKindAgentImage,
		Image:             "test/image:latest",
		BundleMountTarget: "/opt/axern/agents/test",
		Command:           sandbox.ShellCommand("true"),
	}
	_, err := (MountedBundleLauncher{}).Launch(context.Background(), sb, plan)
	if err == nil || !strings.Contains(err.Error(), "agent bundle self-check") {
		t.Fatalf("Launch error = %v", err)
	}
	if len(sb.commands) != 1 {
		t.Fatalf("exec count = %d, want only self-check", len(sb.commands))
	}
}

func TestMountedBundleLauncherRejectsEmptyImage(t *testing.T) {
	plan := LaunchPlan{
		LauncherKind: domain.AgentLauncherKindAgentImage,
		Command:      sandbox.ShellCommand("true"),
	}
	_, err := (MountedBundleLauncher{}).Launch(context.Background(), &fakeSandbox{}, plan)
	if err == nil {
		t.Fatal("expected error for empty image")
	}
}

func TestMountedBundleLauncherRejectsEmptyMountTarget(t *testing.T) {
	plan := LaunchPlan{
		LauncherKind: domain.AgentLauncherKindAgentImage,
		Image:        "test/image:latest",
		Command:      sandbox.ShellCommand("true"),
	}
	_, err := (MountedBundleLauncher{}).Launch(context.Background(), &fakeSandbox{}, plan)
	if err == nil {
		t.Fatal("expected error for empty mount target")
	}
}

func TestAgentImageValidateRejectsWithoutCommand(t *testing.T) {
	plan := LaunchPlan{
		LauncherKind:      domain.AgentLauncherKindAgentImage,
		Image:             "test/image:latest",
		BundleMountTarget: "/opt/axern/agents/test",
	}
	if err := plan.Validate(); err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestAgentImageValidateRejectsMountTargetOutsideBundleRoot(t *testing.T) {
	plan := LaunchPlan{
		LauncherKind:      domain.AgentLauncherKindAgentImage,
		Image:             "test/image:latest",
		BundleMountTarget: "/opt/axern",
		Command:           sandbox.ShellCommand("true"),
	}
	if err := plan.Validate(); err == nil {
		t.Fatal("expected error for mount target outside bundle root")
	}
}

func TestAgentBundleMountTargetSanitizesAgentName(t *testing.T) {
	tests := map[string]string{
		"claude-code":      "/opt/axern/agents/claude-code",
		"Claude Code":      "/opt/axern/agents/claude-code",
		"custom/agent.v1":  "/opt/axern/agents/custom-agent-v1",
		".":                "/opt/axern/agents/agent",
		"..":               "/opt/axern/agents/agent",
		"../../etc/passwd": "/opt/axern/agents/etc-passwd",
	}
	for name, want := range tests {
		if got := AgentBundleMountTarget(name); got != want {
			t.Fatalf("AgentBundleMountTarget(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestMountedBundleLauncherRejectsNilSandbox(t *testing.T) {
	plan := LaunchPlan{
		LauncherKind:      domain.AgentLauncherKindAgentImage,
		Image:             "test/image:latest",
		BundleMountTarget: "/opt/axern/agents/test",
		Command:           sandbox.ShellCommand("true"),
	}
	_, err := (MountedBundleLauncher{}).Launch(context.Background(), nil, plan)
	if err == nil {
		t.Fatal("expected error for nil sandbox")
	}
}

func TestMountedBundleLauncherKind(t *testing.T) {
	if (MountedBundleLauncher{}).Kind() != domain.AgentLauncherKindAgentImage {
		t.Fatal("MountedBundleLauncher.Kind() mismatch")
	}
}

type fakeSandbox struct {
	result   sandbox.ExecResult
	results  []sandbox.ExecResult
	command  sandbox.ExecCommand
	commands []sandbox.ExecCommand
	options  sandbox.ExecOptions
}

func (f *fakeSandbox) Exec(_ context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	f.command = command
	f.commands = append(f.commands, command)
	f.options = options
	if len(f.results) > 0 {
		result := f.results[0]
		f.results = f.results[1:]
		return result, nil
	}
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
