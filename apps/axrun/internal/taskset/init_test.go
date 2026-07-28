package taskset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitBuildsWithExplicitStarterResources(t *testing.T) {
	root := t.TempDir()
	result, err := Init(InitParams{OutputDir: filepath.Join(root, "source")})
	if err != nil {
		t.Fatal(err)
	}

	built, err := Build(BuildParams{
		File:   result.BuildFile,
		Output: filepath.Join(root, "bundle"),
	})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(built.Output, "descriptor.json"))
	if err != nil {
		t.Fatal(err)
	}
	var descriptor Descriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(descriptor.Tasks))
	}
	resources := descriptor.Tasks[0].Instance.Resources
	if resources == nil {
		t.Fatal("starter task resources are missing")
	}
	if resources.RequestCPU != "250m" || resources.RequestMemory != "512Mi" {
		t.Fatalf("starter task resources = %#v", resources)
	}
}
