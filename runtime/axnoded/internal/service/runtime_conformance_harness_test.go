package service

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func TestCLIHarnessCertifiesBeforeImageImport(t *testing.T) {
	script, err := filepath.Abs("../../../../scripts/cli-e2e/environment.sh")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	gate := strings.LastIndex(string(data), "! cli_runtime_capabilities_ready")
	importAt := strings.Index(string(data), "    import_python_runtime_image_once")
	if gate < 0 || importAt < gate {
		t.Fatal("image import races runtime certification")
	}
	platforms := []capabilityv1.PlatformCapability{
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
	}
	for unavailable := -1; unavailable < len(platforms); unavailable++ {
		observations := make([]map[string]any, 0, len(platforms))
		// Runc is not a production prerequisite. Its available observations must
		// neither be required nor satisfy a missing runsc proof.
		for _, platform := range []capabilityv1.PlatformCapability{
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT,
			capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT,
		} {
			if unavailable >= 0 {
				observations = append(observations, map[string]any{"key": map[string]any{"Kind": map[string]any{"Platform": platform}}, "state": capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE})
			}
		}
		for index, platform := range platforms {
			state := capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
			if index == unavailable {
				state = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
			}
			observations = append(observations, map[string]any{"key": map[string]any{"Kind": map[string]any{"Platform": platform}}, "state": state})
		}
		payload, err := json.Marshal(map[string]any{"nodes": []any{map[string]any{
			"node_id": "test-node", "fresh": true, "summary": map[string]any{
				"capability_snapshot": map[string]any{"observations": observations},
			},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command("bash", "-c", "source \"$1\"; cli_runtime_capabilities_ready test-node \"$2\"", "test", script, string(payload))
		if err := command.Run(); (err == nil) != (unavailable == -1) {
			t.Fatalf("unavailable index %d: readiness error=%v", unavailable, err)
		}
	}
}

func TestConformanceHarnessChecksSelectedRuntime(t *testing.T) {
	data, err := os.ReadFile("../../scripts/verify/verify-in-container.sh")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)if jq -e --arg runtime [^\n]+ '\n(.*?)\n    ' <<<`).FindSubmatch(data)
	if len(match) != 2 {
		t.Fatal("selected-runtime conformance gate not found")
	}
	for _, test := range []struct {
		name, runtime, observed string
		missingStorage          bool
		cleanupDebt             int
		ready                   bool
	}{
		{name: "runsc only", runtime: "RUNSC", observed: "RUNSC", ready: true},
		{name: "explicit runc diagnostic", runtime: "RUNC", observed: "RUNC", ready: true},
		{name: "no fallback", runtime: "RUNSC", observed: "RUNC"},
		{name: "storage proof required", runtime: "RUNSC", observed: "RUNSC", missingStorage: true},
		{name: "cleanup must converge", runtime: "RUNSC", observed: "RUNSC", cleanupDebt: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			observations := []any{map[string]any{
				"key":   map[string]any{"platform": "PLATFORM_CAPABILITY_" + test.observed + "_MEMORY_HARD_LIMIT"},
				"state": "CAPABILITY_STATE_AVAILABLE",
			}}
			if !test.missingStorage {
				observations = append(observations, map[string]any{
					"key":   map[string]any{"platform": "PLATFORM_CAPABILITY_" + test.observed + "_EPHEMERAL_STORAGE_HARD_LIMIT"},
					"state": "CAPABILITY_STATE_AVAILABLE",
				})
			}
			payload, err := json.Marshal(map[string]any{"node": map[string]any{
				"memory_budget": map[string]any{
					"mode": "cgroup_v2", "conformance_limit_bytes": 536870912,
					"local_commitment_bytes": 0, "conformance_commitment_bytes": 0,
					"conformance_cleanup_debt_bytes": test.cleanupDebt,
				},
				"capability_snapshot": map[string]any{"observations": observations},
			}})
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command("jq", "-e", "--arg", "runtime", test.runtime, string(match[1]))
			command.Stdin = bytes.NewReader(payload)
			if output, err := command.CombinedOutput(); (err == nil) != test.ready {
				t.Fatalf("readiness error=%v output=%s", err, output)
			}
		})
	}
}
