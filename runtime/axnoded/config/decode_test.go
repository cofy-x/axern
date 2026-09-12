package config

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeRuntimeInstances(t *testing.T) {
	cfg, err := Decode([]byte(`
[plugin.runtime.runtimes.runsc]
binary = "/custom/runsc"
base_spec = "/custom/runsc.json"
[plugin.runtime.runtimes.runsc.options]
allow_suid = false
`))
	if err != nil {
		t.Fatal(err)
	}
	runsc := cfg.RuntimeConfig.Runtimes[RuntimeNameRunsc]
	if len(cfg.RuntimeConfig.Runtimes) != 1 || runsc.Binary != "/custom/runsc" || runsc.BaseSpec != "/custom/runsc.json" || runsc.Options.AllowSUIDEnabled(true) {
		t.Fatalf("runtime configuration = %+v", cfg.RuntimeConfig.Runtimes)
	}
	if cfg.RuntimeConfig.CgroupEnforcement != CgroupEnforcementRequired {
		t.Fatal("omitted enforcement policy lost its production default")
	}
}

func TestDecodeRejectsUnknownAndRemovedSettings(t *testing.T) {
	for _, input := range []string{
		"[plugin.runtime.runtime_binary]\nrunsc = '/legacy/runsc'",
		"[plugin.runtime.basic_spec]\nrunsc = '/legacy/runsc.json'",
		"[plugin.runtime]\nvolume_manager_socket = '/legacy/volumed.sock'",
		"[plugin.runtime]\ncgroup_enforcment = 'disabled_dev'",
		"[plugin.runtime.runtimes.runsc]\nbinry = '/custom/runsc'",
	} {
		if _, err := Decode([]byte(input)); err == nil || !strings.Contains(err.Error(), "undecoded keys") {
			t.Fatalf("invalid configuration %q: %v", input, err)
		}
	}
}

func TestDecodeSampleConfiguration(t *testing.T) {
	data, err := os.ReadFile("../docs/sample_conf.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(data); err != nil {
		t.Fatalf("sample configuration does not match the node contract: %v", err)
	}
}
