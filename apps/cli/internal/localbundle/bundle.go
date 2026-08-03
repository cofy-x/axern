package localbundle

import (
	_ "embed"
	"strings"
)

// imageLock is injected into release binaries as semicolon-separated
// KEY=image@sha256 entries. Development binaries deliberately fall back to
// version tags so source-built images remain usable.
var imageLock string

// Compose is the canonical local deployment bundled into every axern binary.
//
//go:embed assets/compose.yaml
var Compose []byte

// CollectorConfig configures the optional observability profile.
//
//go:embed assets/otel-collector.yaml
var CollectorConfig []byte

func ImageReferences(version string) map[string]string {
	tag := "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "dev" || strings.TrimSpace(version) == "" {
		tag = "dev"
	}
	registry := "ghcr.io/cofy-x/axern/"
	values := map[string]string{
		"POSTGRES_IMAGE":             "postgres:16-alpine",
		"MINIO_IMAGE":                "minio/minio:RELEASE.2025-02-28T09-55-16Z",
		"CONTROLD_IMAGE":             registry + "controld:" + tag,
		"TUNNELD_IMAGE":              registry + "tunneld:" + tag,
		"GATEWAYD_IMAGE":             registry + "gatewayd:" + tag,
		"NODE_ALL_IN_ONE_IMAGE":      registry + "node-all-in-one:" + tag,
		"PYTHON311_RUNTIME_IMAGE":    registry + "python311-runtime:" + tag,
		"SERVER_BASE_RUNTIME_IMAGE":  registry + "server-base-runtime:" + tag,
		"CODING_BASE_RUNTIME_IMAGE":  registry + "coding-base-runtime:" + tag,
		"DESKTOP_BASE_RUNTIME_IMAGE": registry + "desktop-base-runtime:" + tag,
		"CLAUDE_CODE_BUNDLE_IMAGE":   registry + "claude-code-bundle:" + tag,
		"CODEX_BUNDLE_IMAGE":         registry + "codex-bundle:" + tag,
		"OTEL_COLLECTOR_IMAGE":       "otel/opentelemetry-collector:0.150.1",
		"OTEL_LGTM_IMAGE":            "grafana/otel-lgtm:0.11.16",
	}
	for _, entry := range strings.Split(imageLock, ";") {
		key, value, ok := strings.Cut(entry, "=")
		if ok && values[key] != "" && strings.Contains(value, "@sha256:") {
			values[key] = value
		}
	}
	return values
}
