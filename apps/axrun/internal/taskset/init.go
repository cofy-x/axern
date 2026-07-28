package taskset

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"gopkg.in/yaml.v3"
)

type InitParams struct {
	OutputDir string
}

type InitResult struct {
	OutputDir string `json:"output_dir"`
	BuildFile string `json:"build_file"`
}

func Init(params InitParams) (InitResult, error) {
	outputValue := strings.TrimSpace(params.OutputDir)
	if outputValue == "" || filepath.Clean(outputValue) == "." {
		return InitResult{}, fmt.Errorf("output directory is required")
	}
	output, err := filepath.Abs(filepath.Clean(outputValue))
	if err != nil {
		return InitResult{}, err
	}
	if _, err := os.Stat(output); err == nil {
		return InitResult{}, fmt.Errorf("output directory %q already exists", output)
	} else if !os.IsNotExist(err) {
		return InitResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return InitResult{}, err
	}
	stage, err := os.MkdirTemp(filepath.Dir(output), ".axrun-task-init-*")
	if err != nil {
		return InitResult{}, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	workspace := filepath.Join(stage, "workspace")
	verifier := filepath.Join(stage, "verifier")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return InitResult{}, err
	}
	if err := os.MkdirAll(verifier, 0o755); err != nil {
		return InitResult{}, err
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("Complete the task in this workspace.\n"), 0o644); err != nil {
		return InitResult{}, err
	}
	if err := os.WriteFile(filepath.Join(verifier, "check.sh"), []byte("#!/usr/bin/env bash\nset -euo pipefail\ntest -f /workspace/answer.txt\n"), 0o755); err != nil {
		return InitResult{}, err
	}
	spec := BuildEnvelope{
		APIVersion: APIVersion,
		Kind:       BuildKind,
		Metadata:   Metadata{Name: filepath.Base(output)},
		Spec: BuildSpec{Generators: []Generator{{
			TaskID:      "example",
			Instruction: Instruction{Text: "Create answer.txt in the workspace."},
			Workspace:   WorkspaceSource{Paths: []string{"workspace"}, Expand: "aggregate"},
			Task: TaskTemplate{
				Resources: &domain.ResourceSpec{
					RequestCPU:    "250m",
					RequestMemory: "512Mi",
				},
				Sandbox: domain.SandboxSpec{
					Backend: domain.SandboxBackendAxern,
					Workdir: "/workspace",
					RuntimeSource: &domain.SandboxRuntimeSourceSpec{
						Type:       domain.SandboxRuntimeSourceTemplate,
						TemplateID: "python311",
					},
				},
				Verifier: domain.VerifierSpec{
					Type:    domain.VerifierTypeShell,
					Command: "bash /workspace/.axrun/verifier/check.sh",
					CWD:     "/workspace",
					Assets: []domain.VerifierAssetSpec{
						{
							Path:       "verifier/check.sh",
							TargetPath: "/workspace/.axrun/verifier/check.sh",
						},
					},
				},
				Outputs: []OutputContract{{Path: "answer.txt", Required: true}},
			},
		}}},
	}
	data, err := yaml.Marshal(spec)
	if err != nil {
		return InitResult{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "taskset.yaml"), data, 0o644); err != nil {
		return InitResult{}, err
	}
	if err := os.Rename(stage, output); err != nil {
		return InitResult{}, fmt.Errorf("commit initialized TaskSet: %w", err)
	}
	return InitResult{OutputDir: output, BuildFile: filepath.Join(output, "taskset.yaml")}, nil
}
