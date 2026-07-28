package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestValidateRunRejectsAmbiguousRuntimeSource(t *testing.T) {
	runDir := createSchemaFixture(t)
	inputDir := filepath.Join(runDir, "inputs", "task-dir")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("mkdir input dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	taskPath := filepath.Join(runDir, "tasks", "task", "task.json")
	var task domain.TaskInstance
	readSchemaJSON(t, taskPath, &task)
	task.Sandbox.RuntimeSource = &domain.SandboxRuntimeSourceSpec{
		Type:       domain.SandboxRuntimeSourceImage,
		Image:      "example.com/task:latest",
		Dockerfile: "inputs/task-dir/Dockerfile",
	}
	writeSchemaJSON(t, taskPath, task)

	result, err := ValidateRun(Params{RunDir: runDir})
	if err == nil {
		t.Fatal("ValidateRun error = nil")
	}
	if result.Valid() || !containsProblem(result, "sandbox.runtime_source.dockerfile", "must be empty") {
		t.Fatalf("result = %#v", result)
	}
}
