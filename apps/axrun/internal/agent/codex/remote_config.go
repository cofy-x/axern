package codex

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
)

const (
	defaultCodexProviderName = "axern"
)

func writeRemoteCodexConfig(profile agent.Profile) (string, error) {
	configTOML, err := codexConfigTOML(profile)
	if err != nil {
		return "", err
	}
	return remoteCodexConfigScript(configTOML), nil
}

func codexConfigTOML(profile agent.Profile) (string, error) {
	providerName := configValue(profile, "codex_provider", defaultCodexProviderName)
	wireAPI := string(profile.WireAPI)
	if err := validateCodexWireAPI(wireAPI); err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("model_provider = ")
	builder.WriteString(tomlQuote(providerName))
	builder.WriteByte('\n')
	builder.WriteString(`openai_base_url = "${AXERN_MANAGED_PROXY_BASE_URL}"`)
	builder.WriteString("\n\n")
	builder.WriteString("[model_providers.")
	builder.WriteString(tomlKey(providerName))
	builder.WriteString("]\n")
	builder.WriteString("name = ")
	builder.WriteString(tomlQuote("Axern Axrun Proxy"))
	builder.WriteByte('\n')
	builder.WriteString(`base_url = "${AXERN_MANAGED_PROXY_BASE_URL}"`)
	builder.WriteByte('\n')
	builder.WriteString("env_key = ")
	builder.WriteString(tomlQuote("OPENAI_API_KEY"))
	builder.WriteByte('\n')
	builder.WriteString("wire_api = ")
	builder.WriteString(tomlQuote(wireAPI))
	builder.WriteByte('\n')
	return builder.String(), nil
}

func remoteCodexConfigScript(configTOML string) string {
	return fmt.Sprintf(`set -eu
: "${AXERN_MANAGED_PROXY_BASE_URL:?}"
: "${AXERN_MANAGED_PROXY_TOKEN:?}"
export OPENAI_BASE_URL="${AXERN_MANAGED_PROXY_BASE_URL}"
export OPENAI_API_KEY="${AXERN_MANAGED_PROXY_TOKEN}"
codex_home="${CODEX_HOME:-${HOME}/.codex}"
mkdir -p "${codex_home}"
stamp="$(date +%%Y%%m%%d%%H%%M%%S)"
if [ -f "${codex_home}/config.toml" ]; then
  cp "${codex_home}/config.toml" "${codex_home}/config.toml.axern.bak.${stamp}"
fi
cat > "${codex_home}/config.toml" <<AXERN_CODEX_CONFIG
%s
AXERN_CODEX_CONFIG
`, configTOML)
}

func configValue(profile agent.Profile, key string, fallback string) string {
	if profile.Config != nil {
		if value := strings.TrimSpace(profile.Config[key]); value != "" {
			return value
		}
	}
	return fallback
}

func validateCodexWireAPI(value string) error {
	switch strings.TrimSpace(value) {
	case "responses":
		return nil
	default:
		return fmt.Errorf("codex wire_api must be responses, got %q", value)
	}
}

func tomlKey(value string) string {
	if isTOMLBareKey(value) {
		return value
	}
	return tomlQuote(value)
}

func isTOMLBareKey(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func tomlQuote(value string) string {
	return strconv.Quote(value)
}
