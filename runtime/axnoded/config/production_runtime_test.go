package config

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestProductionRuntimeDefaultsEnableOnlyRunsc(t *testing.T) {
	runtimes := DefaultConfig().PluginConfig.RuntimeConfig.Runtimes
	if len(runtimes) != 1 || runtimes[RuntimeNameRunsc].Binary == "" {
		t.Fatalf("production defaults must enable only runsc: %v", runtimes)
	}

	// These generators configure the packaged node and the source-development
	// stack. An installed diagnostic binary must not re-enable its handler.
	header := regexp.MustCompile(`(?m)^\[plugin\.runtime\.runtimes\.([^.\]]+)\]$`)
	for _, relative := range []string{
		"deploy/images/lib/node-all-in-one-entrypoint.sh",
		"scripts/devbox/node-dev-prepare.sh",
		"runtime/axnoded/docs/sample_conf.toml",
	} {
		t.Run(relative, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("../../..", relative))
			if err != nil {
				t.Fatal(err)
			}
			matches := header.FindAllSubmatch(data, -1)
			if len(matches) != 1 || string(matches[0][1]) != RuntimeNameRunsc {
				t.Fatalf("production generator must configure only runsc: %q", matches)
			}
		})
	}
}
