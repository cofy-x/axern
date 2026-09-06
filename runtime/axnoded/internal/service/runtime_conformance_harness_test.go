package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_MEMORY_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNC_EPHEMERAL_STORAGE_HARD_LIMIT,
		capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT,
	}
	for unavailable := -1; unavailable < len(platforms); unavailable++ {
		observations := make([]map[string]any, 0, len(platforms))
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
