package claudecode

import (
	"encoding/json"
	"fmt"
)

func writeRemoteClaudeConfig(profile Profile) (string, error) {
	snippets, err := claudeCodeConfigSnippets(profile)
	if err != nil {
		return "", err
	}
	return remoteConfigScript(snippets), nil
}

type configSnippets struct {
	SettingsJSON   string
	UserConfigJSON string
}

func claudeCodeConfigSnippets(profile Profile) (configSnippets, error) {
	settings, err := json.MarshalIndent(map[string]map[string]string{
		"env": claudeCodeSettingsEnv(profile),
	}, "", "  ")
	if err != nil {
		return configSnippets{}, err
	}
	userConfig, err := json.MarshalIndent(map[string]bool{"hasCompletedOnboarding": true}, "", "  ")
	if err != nil {
		return configSnippets{}, err
	}
	return configSnippets{
		SettingsJSON:   string(settings),
		UserConfigJSON: string(userConfig),
	}, nil
}

func claudeCodeSettingsEnv(profile Profile) map[string]string {
	env := map[string]string{
		"ANTHROPIC_BASE_URL":                       "${AXERN_MANAGED_PROXY_BASE_URL}",
		"ANTHROPIC_API_KEY":                        "${AXERN_MANAGED_PROXY_TOKEN}",
		"CLAUDE_CODE_SIMPLE":                       "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	}
	if profile.HaikuModel != "" {
		env["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = profile.HaikuModel
	}
	if profile.SonnetModel != "" {
		env["ANTHROPIC_DEFAULT_SONNET_MODEL"] = profile.SonnetModel
	}
	if profile.OpusModel != "" {
		env["ANTHROPIC_DEFAULT_OPUS_MODEL"] = profile.OpusModel
	}
	if profile.APITimeoutMS != "" {
		env["API_TIMEOUT_MS"] = profile.APITimeoutMS
	}
	return env
}

func remoteConfigScript(snippets configSnippets) string {
	return fmt.Sprintf(`set -eu
: "${AXERN_MANAGED_PROXY_BASE_URL:?}"
: "${AXERN_MANAGED_PROXY_TOKEN:?}"
mkdir -p "${HOME}/.claude"
stamp="$(date +%%Y%%m%%d%%H%%M%%S)"
if [ -f "${HOME}/.claude/settings.json" ]; then
  cp "${HOME}/.claude/settings.json" "${HOME}/.claude/settings.json.axern.bak.${stamp}"
fi
if [ -f "${HOME}/.claude.json" ]; then
  cp "${HOME}/.claude.json" "${HOME}/.claude.json.axern.bak.${stamp}"
fi
cat > "${HOME}/.claude/settings.json" <<AXERN_SETTINGS_JSON
%s
AXERN_SETTINGS_JSON
cat > "${HOME}/.claude.json" <<'AXERN_USER_JSON'
%s
AXERN_USER_JSON
`, snippets.SettingsJSON, snippets.UserConfigJSON)
}
