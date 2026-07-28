package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appagent "github.com/cofy-x/axern/apps/cli/internal/application/agent"
	"github.com/cofy-x/axern/apps/cli/internal/command"
	"github.com/cofy-x/axern/apps/cli/internal/output"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	"github.com/spf13/cobra"
)

func TestRemoteConfigUsesPersistentWorkspaceWithoutProviderToken(t *testing.T) {
	upstream, _ := url.Parse("https://api.example.test/v1")
	script, err := remoteConfigScript(35748, agentprofile.Profile{
		Agent: agentprofile.AgentCodex, ProviderType: agentprofile.ProviderOpenAI,
		WireAPI: agentprofile.WireAPIResponses, Upstream: upstream, Token: "provider-secret",
	}, "session-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`agent_workspace="/home/axern/workspace"`, `export AXERN_AGENT_WORKSPACE="${agent_workspace}"`, `sudo chown`, `sudo chmod 0777`, `test -w`} {
		if !strings.Contains(script, expected) {
			t.Fatalf("remote script missing %q:\n%s", expected, script)
		}
	}
	if strings.Contains(script, "provider-secret") || strings.Contains(script, "chown -R") || strings.Contains(script, "chmod -R") || strings.Contains(script, "${HOME}/workspace") || strings.Contains(script, "git init") || strings.Contains(script, "install -d") || strings.Contains(script, `mkdir -p "${agent_workspace}"`) {
		t.Fatalf("remote script contains forbidden data or behavior:\n%s", script)
	}
}

func TestDoctorReturnsConfigExitCodeForInvalidProfile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configPath, []byte(`{
  "agent_profiles": {
    "current_profile": "old-codex",
    "profiles": {
      "old-codex": {
        "agent": "codex",
        "provider": "openai",
        "upstream": "https://api.example.test/v1",
        "token": "secret"
      }
    }
  }
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := doctorCommand(command.Runtime{Options: &command.Options{ConfigPath: configPath, Output: "table"}})
	var output bytes.Buffer
	cmd.SetOut(&output)
	err := cmd.RunE(cmd, nil)
	var exitErr command.ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("doctor error = %#v", err)
	}
	if !strings.Contains(output.String(), "wire_api is required") {
		t.Fatalf("doctor output = %q", output.String())
	}
}

func TestProfileSetUsesUnambiguousAgentConfigFlag(t *testing.T) {
	cmd := profileSetCommand(command.Runtime{})
	if cmd.Flags().Lookup("agent-config") == nil {
		t.Fatal("profile set is missing --agent-config")
	}
	if cmd.Flags().Lookup("config") != nil {
		t.Fatal("profile set must not shadow the global --config flag")
	}
}

func TestWorkspaceFlagIsAvailableOnWorkspaceCommands(t *testing.T) {
	root := Command(command.Runtime{})
	for _, name := range []string{"shell", "run", "connect", "doctor", "list", "stop"} {
		child, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if child.Flags().Lookup("workspace") == nil {
			t.Fatalf("agent %s is missing --workspace", name)
		}
	}
}

func TestWorkspaceDeleteRequiresExplicitWorkspaceAndAutomationConfirmation(t *testing.T) {
	cmd := workspaceDeleteCommand(command.Runtime{})
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--workspace is required") {
		t.Fatalf("missing workspace error = %v", err)
	}

	cmd = workspaceDeleteCommand(command.Runtime{})
	if err := cmd.Flags().Set("workspace", "project-a"); err != nil {
		t.Fatal(err)
	}
	cmd.SetIn(strings.NewReader("project-a\n"))
	err = cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--yes is required for non-interactive deletion") {
		t.Fatalf("non-interactive error = %v", err)
	}
}

func TestWorkspaceDeleteRejectsNegativeTimeoutBeforeOpeningSession(t *testing.T) {
	cmd := workspaceDeleteCommand(command.Runtime{})
	if err := cmd.Flags().Set("workspace", "project-a"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("yes", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("timeout", "-1s"); err != nil {
		t.Fatal(err)
	}
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--timeout must be non-negative") {
		t.Fatalf("negative timeout error = %v", err)
	}
}

func TestRuntimeListJSONUsesStableWorkspaceLifecycle(t *testing.T) {
	var buffer bytes.Buffer
	err := renderRuntimeList(&buffer, []appagent.RuntimeSummary{{
		Workspace: "project-a", LifecycleState: appagent.LifecycleSuspended, Persistent: true,
		ServiceID: "svc-1", Profile: "dev-codex", Agent: "codex", Namespace: "default",
		Ready: 0, Desired: 0, EnvironmentID: "env-1",
	}}, output.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0]["workspace"] != "project-a" || items[0]["lifecycle_state"] != "suspended" || items[0]["persistent"] != true {
		t.Fatalf("runtime JSON = %#v", items)
	}
	if _, ok := items[0]["status"]; ok {
		t.Fatalf("runtime JSON contains unstable service status: %#v", items[0])
	}
}

func TestDefaultModelConfigKeyMatchesAgentRuntime(t *testing.T) {
	if got := defaultModelConfigKey(agentprofile.AgentCodex); got != "model" {
		t.Fatalf("Codex model config key = %q", got)
	}
	if got := defaultModelConfigKey(agentprofile.AgentClaudeCode); got != "sonnet_model" {
		t.Fatalf("Claude Code model config key = %q", got)
	}
}

func TestRenderDoctorJSONIsStableAndContainsNoCredentials(t *testing.T) {
	result := appagent.DoctorResult{
		Agent:               "claude-code",
		Provider:            "anthropic",
		Profile:             "production",
		Workspace:           "project-a",
		WorkspaceTemplate:   "coding-base",
		AgentBundle:         "claude-code",
		ConfigOK:            true,
		ApprovalCompatible:  true,
		AxernApprovalPolicy: "never",
		LocalApprovalPolicy: "on_request",
		ServiceID:           "svc-1",
		ReadyReplicas:       1,
		DesiredReplicas:     1,
		LifecycleState:      appagent.LifecycleRunning,
		Persistent:          true,
		Recommendation:      "agent workspace is ready",
		PlatformCheck:       &appagent.DoctorPlatformCheck{Reachable: true, Message: "Axern platform API is reachable"},
	}
	var buffer bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&buffer)
	if err := renderDoctor(command, result, output.FormatJSON); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	want := []string{"agent", "agent_bundle", "approval_compatible", "axern_approval_policy", "config_ok", "desired_replicas", "lifecycle_state", "local_approval_policy", "persistent", "platform_check", "profile", "provider", "ready_replicas", "recommendation", "service_id", "workspace", "workspace_template"}
	if len(fields) != len(want) {
		t.Fatalf("doctor JSON fields = %#v", fields)
	}
	for _, field := range want {
		if _, ok := fields[field]; !ok {
			t.Fatalf("doctor JSON missing %q: %#v", field, fields)
		}
	}
	for _, sensitive := range []string{"token", "secret", "upstream"} {
		if strings.Contains(strings.ToLower(buffer.String()), sensitive) {
			t.Fatalf("doctor JSON contains sensitive field %q: %s", sensitive, buffer.String())
		}
	}
}

func TestRenderDoctorTableShowsWorkspaceTemplateAndAgentBundle(t *testing.T) {
	result := appagent.DoctorResult{
		Agent:             "claude-code",
		Provider:          "anthropic",
		Profile:           "production",
		Workspace:         "project-a",
		WorkspaceTemplate: "coding-base",
		AgentBundle:       "claude-code",
	}
	var buffer bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&buffer)
	if err := renderDoctor(command, result, output.FormatTable); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Workspace template: coding-base", "Agent bundle: claude-code"} {
		if !strings.Contains(buffer.String(), expected) {
			t.Fatalf("doctor output missing %q: %s", expected, buffer.String())
		}
	}
	if strings.Contains(buffer.String(), "Runtime:") {
		t.Fatalf("doctor output contains obsolete runtime label: %s", buffer.String())
	}
}
