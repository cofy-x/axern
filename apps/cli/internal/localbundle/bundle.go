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

func ImageReferences(version string) map[string]string {
	tag := "v" + strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "dev" || strings.TrimSpace(version) == "" {
		tag = "dev"
	}
	registry := "ghcr.io/cofy-x/axern/"
	values := map[string]string{
		"POSTGRES_IMAGE":        registry + "postgres:" + tag,
		"REGISTRY_IMAGE":        registry + "registry:" + tag,
		"CONTROLD_IMAGE":        registry + "controld:" + tag,
		"TUNNELD_IMAGE":         registry + "tunneld:" + tag,
		"GATEWAYD_IMAGE":        registry + "gatewayd:" + tag,
		"NODE_ALL_IN_ONE_IMAGE": registry + "node-all-in-one:" + tag,
	}
	for _, entry := range strings.Split(imageLock, ";") {
		key, value, ok := strings.Cut(entry, "=")
		if ok && values[key] != "" && strings.Contains(value, "@sha256:") {
			values[key] = value
		}
	}
	return values
}
