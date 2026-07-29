package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	appagent "github.com/cofy-x/axern/apps/cli/internal/application/agent"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	"github.com/cofy-x/axern/sdk/go/clientconfig"
)

func resolveRemoteTarget(profile *clientconfig.Context, user, target, key string, strict bool) (appagent.RemoteTarget, error) {
	if profile != nil {
		if target == "" {
			target = profile.SSHEndpoint
		}
		if key == "" {
			key = profile.SSHIdentityFile
		}
	}
	if target == "" {
		return appagent.RemoteTarget{}, fmt.Errorf("SSH endpoint is not configured")
	}
	if key == "" {
		return appagent.RemoteTarget{}, fmt.Errorf("SSH identity file is not configured")
	}
	return appagent.RemoteTarget{
		User:                  user,
		SSHTarget:             target,
		SSHKey:                key,
		StrictHostKeyChecking: strict,
	}, nil
}

type sshRemoteRunner struct{}

func (sshRemoteRunner) WriteAgentConfig(ctx context.Context, target appagent.RemoteTarget, remotePort int32, profile agentprofile.Profile, localToken string) error {
	script, err := remoteConfigScript(remotePort, profile, localToken)
	if err != nil {
		return err
	}
	return runRemoteScript(ctx, target, script, "write agent remote config")
}

func (sshRemoteRunner) RestoreAgentConfig(ctx context.Context, target appagent.RemoteTarget, agentType agentprofile.AgentType) error {
	return runRemoteScript(ctx, target, remoteRestoreScript(agentType), "restore agent remote config")
}

func (sshRemoteRunner) Run(ctx context.Context, target appagent.RemoteTarget, command string, requestTTY bool) error {
	return runRemoteCommand(ctx, target, command, requestTTY)
}

func runRemoteCommand(ctx context.Context, target appagent.RemoteTarget, command string, requestTTY bool) error {
	args, err := sshArgs(target, command, requestTTY)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = remoteCommandStdin(os.Stdin, requestTTY)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func remoteCommandStdin(stdin *os.File, requestTTY bool) io.Reader {
	if requestTTY {
		return stdin
	}
	if stdin == nil {
		return nil
	}
	info, err := stdin.Stat()
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	return stdin
}

func runRemoteScript(ctx context.Context, target appagent.RemoteTarget, script, action string) error {
	args, err := sshArgs(target, "bash -s", false)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	return nil
}

func sshArgs(target appagent.RemoteTarget, command string, requestTTY bool) ([]string, error) {
	if strings.TrimSpace(target.SSHTarget) == "" {
		return nil, fmt.Errorf("ssh target is required")
	}
	if strings.TrimSpace(target.SSHKey) == "" {
		return nil, fmt.Errorf("ssh identity file is required")
	}
	if strings.TrimSpace(target.AllocationID) == "" {
		return nil, fmt.Errorf("allocation id is required")
	}
	user := strings.TrimSpace(target.User)
	if user == "" {
		user = appagent.DefaultRemoteUser
	}
	args := []string{
		"-i", target.SSHKey,
		"-p", sshPort(target.SSHTarget),
		"-o", "IdentitiesOnly=yes",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ProxyCommand=none",
		"-o", "SetEnv=AXERN_EXEC_USER=" + user,
	}
	if !target.StrictHostKeyChecking {
		args = append(args, "-o", "StrictHostKeyChecking=no")
	}
	if requestTTY {
		args = append(args, "-tt")
	}
	args = append(args, target.AllocationID+"@"+sshHost(target.SSHTarget), command)
	return args, nil
}

func sshHost(target string) string {
	host, _, ok := strings.Cut(target, ":")
	if !ok || host == "" {
		return target
	}
	return host
}

func sshPort(target string) string {
	_, port, ok := strings.Cut(target, ":")
	if !ok || port == "" {
		return "22"
	}
	return port
}

func remoteConfigScript(remotePort int32, profile agentprofile.Profile, localToken string) (string, error) {
	var script string
	var err error
	switch profile.Agent {
	case agentprofile.AgentCodex:
		script, err = codexRemoteConfigScript(remotePort, profile, localToken)
	case agentprofile.AgentClaudeCode:
		script, err = claudeRemoteConfigScript(remotePort, profile, localToken)
	default:
		return "", fmt.Errorf("unsupported agent %q", profile.Agent)
	}
	if err != nil {
		return "", err
	}
	return agentWorkspaceScript() + script, nil
}

func agentWorkspaceScript() string {
	return `set -eu
agent_workspace="/home/axern/workspace"
if [ ! -d "${agent_workspace}" ]; then
  printf 'agent workspace volume is not mounted at %s\n' "${agent_workspace}" >&2
  exit 1
fi
if [ ! -w "${agent_workspace}" ]; then
  sudo chown "$(id -u):$(id -g)" "${agent_workspace}"
  if [ ! -w "${agent_workspace}" ]; then
    sudo chmod 0777 "${agent_workspace}"
  fi
fi
test -w "${agent_workspace}"
export AXERN_AGENT_WORKSPACE="${agent_workspace}"
`
}

func codexRemoteConfigScript(remotePort int32, profile agentprofile.Profile, localToken string) (string, error) {
	if err := agentprofile.ValidateWireAPI(profile.Agent, profile.WireAPI); err != nil {
		return "", err
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", remotePort)
	model := strings.TrimSpace(profile.Config["model"])
	configTOML := ""
	if model != "" {
		configTOML += "model = " + tomlQuote(model) + "\n"
	}
	configTOML += "model_provider = \"axern\"\n" +
		"openai_base_url = " + tomlQuote(baseURL) + "\n\n" +
		"[model_providers.axern]\n" +
		"name = \"Axern Agent Proxy\"\n" +
		"base_url = " + tomlQuote(baseURL) + "\n" +
		"env_key = \"OPENAI_API_KEY\"\n" +
		"wire_api = " + tomlQuote(string(profile.WireAPI)) + "\n"
	return fmt.Sprintf(`set -eu
export OPENAI_BASE_URL=%s
export OPENAI_API_KEY=%s
codex_home="${CODEX_HOME:-${HOME}/.codex}"
mkdir -p "${codex_home}"
stamp="$(date +%%Y%%m%%d%%H%%M%%S)"
if [ -f "${codex_home}/config.toml" ]; then
  cp "${codex_home}/config.toml" "${codex_home}/config.toml.axern.bak.${stamp}"
fi
cat > "${codex_home}/config.toml" <<'AXERN_CODEX_CONFIG'
%s
AXERN_CODEX_CONFIG
`, shellQuote(baseURL), shellQuote(localToken), configTOML), nil
}

func claudeRemoteConfigScript(remotePort int32, profile agentprofile.Profile, localToken string) (string, error) {
	settings, err := json.MarshalIndent(map[string]map[string]string{
		"env": claudeSettingsEnv(remotePort, profile, localToken),
	}, "", "  ")
	if err != nil {
		return "", err
	}
	userConfig, err := json.MarshalIndent(map[string]bool{"hasCompletedOnboarding": true}, "", "  ")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`set -eu
mkdir -p "${HOME}/.claude"
stamp="$(date +%%Y%%m%%d%%H%%M%%S)"
if [ -f "${HOME}/.claude/settings.json" ]; then
  cp "${HOME}/.claude/settings.json" "${HOME}/.claude/settings.json.axern.bak.${stamp}"
fi
if [ -f "${HOME}/.claude.json" ]; then
  cp "${HOME}/.claude.json" "${HOME}/.claude.json.axern.bak.${stamp}"
fi
cat > "${HOME}/.claude/settings.json" <<'AXERN_SETTINGS_JSON'
%s
AXERN_SETTINGS_JSON
cat > "${HOME}/.claude.json" <<'AXERN_USER_JSON'
%s
AXERN_USER_JSON
`, string(settings), string(userConfig)), nil
}

func claudeSettingsEnv(remotePort int32, profile agentprofile.Profile, localToken string) map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       fmt.Sprintf("http://127.0.0.1:%d", remotePort),
		"ANTHROPIC_API_KEY":                        localToken,
		"CLAUDE_CODE_SIMPLE":                       "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	for _, item := range []struct {
		key string
		env string
	}{
		{"haiku_model", "ANTHROPIC_DEFAULT_HAIKU_MODEL"},
		{"sonnet_model", "ANTHROPIC_DEFAULT_SONNET_MODEL"},
		{"opus_model", "ANTHROPIC_DEFAULT_OPUS_MODEL"},
		{"api_timeout_ms", "API_TIMEOUT_MS"},
	} {
		if value := strings.TrimSpace(profile.Config[item.key]); value != "" {
			env[item.env] = value
		}
	}
	return env
}

func remoteRestoreScript(agentType agentprofile.AgentType) string {
	switch agentType {
	case agentprofile.AgentCodex:
		return `set -eu
codex_home="${CODEX_HOME:-${HOME}/.codex}"
latest="$(ls -1t "${codex_home}"/config.toml.axern.bak.* 2>/dev/null | head -n 1 || true)"
if [ -n "${latest}" ]; then
  cp "${latest}" "${codex_home}/config.toml"
fi
`
	case agentprofile.AgentClaudeCode:
		return `set -eu
latest_settings="$(ls -1t "${HOME}"/.claude/settings.json.axern.bak.* 2>/dev/null | head -n 1 || true)"
latest_user="$(ls -1t "${HOME}"/.claude.json.axern.bak.* 2>/dev/null | head -n 1 || true)"
if [ -n "${latest_settings}" ]; then
  cp "${latest_settings}" "${HOME}/.claude/settings.json"
fi
if [ -n "${latest_user}" ]; then
  cp "${latest_user}" "${HOME}/.claude.json"
fi
`
	default:
		return "set -eu\n"
	}
}

func tomlQuote(value string) string {
	return strconv.Quote(value)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-./:=,+", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
