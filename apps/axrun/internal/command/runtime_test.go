package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	rolloutv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/rollout/v1"
)

func TestPrintValueUsesCanonicalProtoJSON(t *testing.T) {
	var output bytes.Buffer
	value := &rolloutv1.GetRolloutResponse{Rollout: &rolloutv1.Rollout{
		ID:     "rol-test",
		Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED,
	}}
	if err := PrintValue(&output, "json", value, ""); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	rollout, ok := decoded["rollout"].(map[string]any)
	if !ok {
		t.Fatalf("JSON output missing rollout object: %s", output.String())
	}
	if rollout["status"] != "ROLLOUT_STATUS_COMPLETED" {
		t.Fatalf("expected canonical enum name, got %#v", rollout["status"])
	}
}

func TestPrintJSONLineWritesCompactCanonicalProtoJSON(t *testing.T) {
	var output bytes.Buffer
	value := &rolloutv1.Rollout{
		ID:     "rol-test",
		Status: rolloutv1.RolloutStatus_ROLLOUT_STATUS_COMPLETED,
	}
	if err := PrintJSONLine(&output, value); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 1 || !strings.HasSuffix(output.String(), "\n") {
		t.Fatalf("expected exactly one newline-terminated record: %q", output.String())
	}
	if strings.Contains(output.String(), "\n  ") {
		t.Fatalf("expected compact JSON: %q", output.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &decoded); err != nil {
		t.Fatalf("record is not valid JSON: %v", err)
	}
	if decoded["status"] != "ROLLOUT_STATUS_COMPLETED" {
		t.Fatalf("expected canonical enum name, got %#v", decoded["status"])
	}
}
