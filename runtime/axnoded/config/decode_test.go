package config

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeRunsc(t *testing.T) {
	cfg, err := Decode([]byte(`
[plugin.runtime.runsc]
binary = "/custom/runsc"
[plugin.runtime.runsc.options]
allow_suid = false
`))
	if err != nil {
		t.Fatal(err)
	}
	runsc := cfg.RuntimeConfig.Runsc
	if runsc.Binary != "/custom/runsc" || runsc.Options.AllowSUIDEnabled(true) {
		t.Fatalf("runsc configuration = %+v", runsc)
	}
	if cfg.RuntimeConfig.CgroupEnforcement != CgroupEnforcementRequired {
		t.Fatal("omitted enforcement policy lost its production default")
	}
}

func TestDecodeRejectsUnknownAndRemovedSettings(t *testing.T) {
	for _, input := range []string{
		"[plugin.runtime.runsc]\nbase_spec = '/custom/runsc.json'",
		"[plugin.runtime.runtime_binary]\nrunsc = '/legacy/runsc'",
		"[plugin.runtime.basic_spec]\nrunsc = '/legacy/runsc.json'",
		"[plugin.runtime]\nvolume_manager_socket = '/legacy/volumed.sock'",
		"[plugin.runtime]\ncgroup_enforcment = 'disabled_dev'",
		"[plugin.runtime.runsc]\nbinry = '/custom/runsc'",
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

func TestConnectedNodeRequiresExplicitIdentity(t *testing.T) {
	const base = "[plugin]\ncontrol_plane_target = 'controld:24000'\ncontrol_plane_enrollment_target = 'controld:24002'\nworkload_cluster = 'cluster.test'\ncontrol_plane_tls_ca_cert = '/trust.pem'\n"
	for _, id := range []string{"", "node/id", " node-id", "../node"} {
		if _, err := Decode([]byte(base + "control_plane_node_id = '" + id + "'\n")); err == nil {
			t.Fatalf("invalid node identity %q accepted", id)
		}
	}
	if _, err := Decode([]byte(base + "control_plane_node_id = 'explicit-node'\n")); err != nil {
		t.Fatal(err)
	}
}
