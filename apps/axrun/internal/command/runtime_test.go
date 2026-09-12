package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
)

func TestPrintValueUsesCanonicalProtoJSON(t *testing.T) {
	var output bytes.Buffer
	value := &runv1.GetRunResponse{Run: &runv1.Run{
		ID:     "run-test",
		Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED,
	}}
	if err := PrintValue(&output, "json", value, ""); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	run, ok := decoded["run"].(map[string]any)
	if !ok {
		t.Fatalf("JSON output missing run object: %s", output.String())
	}
	if run["status"] != "RUN_STATUS_SUCCEEDED" {
		t.Fatalf("expected canonical enum name, got %#v", run["status"])
	}
}

func TestPrintJSONLineWritesCompactCanonicalProtoJSON(t *testing.T) {
	var output bytes.Buffer
	value := &runv1.Run{
		ID:     "run-test",
		Status: runv1.RunStatus_RUN_STATUS_SUCCEEDED,
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
	if decoded["status"] != "RUN_STATUS_SUCCEEDED" {
		t.Fatalf("expected canonical enum name, got %#v", decoded["status"])
	}
}
