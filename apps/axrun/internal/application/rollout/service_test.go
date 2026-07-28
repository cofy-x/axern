package rollout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

func TestServicePlansTaskSet(t *testing.T) {
	bundle := buildTaskSet(t)
	result, err := (Service{}).Run(Params{
		TaskSetRef:  bundle,
		Agent:       "oracle",
		Model:       "test/model",
		BackendName: "local",
		Concurrency: 1,
		Attempts:    2,
		Output:      t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var run domain.RolloutRun
	readJSON(t, result.RunJSONPath, &run)
	if run.Input == nil || run.Input.Type != domain.InputTypeTaskSet {
		t.Fatalf("input = %#v", run.Input)
	}
	if len(result.Episodes) != 2 || len(run.TaskIDs) != 1 || run.TaskIDs[0] != "example" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := os.Stat(filepath.Join(result.RunDir, "inputs", "taskset-descriptor.json")); err != nil {
		t.Fatalf("captured descriptor: %v", err)
	}
}

func TestNormalizeParamsRequiresTaskSet(t *testing.T) {
	_, err := NormalizeParams(Params{Agent: "oracle", Model: "test/model", BackendName: "local", Concurrency: 1, Attempts: 1, Output: t.TempDir()})
	if err == nil || err.Error() != "task set reference is required" {
		t.Fatalf("error = %v", err)
	}
}

func TestServiceExecutesAttempts(t *testing.T) {
	bundle := buildTaskSet(t)
	result, err := (Service{}).Run(Params{
		TaskSetRef:   bundle,
		Agent:        "command",
		AgentCommand: "printf done > result.txt",
		BackendName:  "local",
		Concurrency:  2,
		Attempts:     3,
		Execute:      true,
		Output:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != domain.RunStatusCompleted || result.TaskCount != 1 || result.EpisodeCount != 3 || result.Summary.CompletedEpisodes != 3 {
		t.Fatalf("result = %#v", result)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		episodeID := domain.NewEpisodeID(result.RunID, "example", attempt)
		if _, err := os.Stat(filepath.Join(result.RunDir, "episodes", episodeID, "episode.json")); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
}

func TestExecutionHonorsConcurrencyLimit(t *testing.T) {
	bundle := buildTaskSetMultiple(t)
	output := t.TempDir()
	enter := make(chan string, 2)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := (Service{BackendFactory: func(BackendRequest) (backend.Backend, error) {
			return trackingBackend{enter: enter, release: release}, nil
		}}).Run(Params{
			TaskSetRef: bundle, Agent: "oracle", Model: "test/model",
			BackendName: "local", Concurrency: 1, Attempts: 1, Execute: true, Output: output,
		})
		done <- err
	}()

	first := <-enter
	select {
	case second := <-enter:
		t.Fatalf("task %q started while task %q still held the only concurrency slot", second, first)
	default:
	}
	release <- struct{}{}
	second := <-enter
	if second == first {
		t.Fatalf("same task entered twice: %q", first)
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestVerifierFailureDoesNotBecomeInfrastructureFailure(t *testing.T) {
	result, err := (Service{BackendFactory: func(BackendRequest) (backend.Backend, error) {
		return taskStatusBackend{statuses: map[string]domain.EpisodeStatus{"task-b": domain.EpisodeStatusFailed}}, nil
	}}).Run(Params{
		TaskSetRef: buildTaskSetMultiple(t), Agent: "oracle", Model: "test/model",
		BackendName: "local", Concurrency: 2, Attempts: 1, Execute: true, Output: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != domain.RunStatusCompleted || result.Summary.CompletedEpisodes != 1 ||
		result.Summary.VerifierFailedEpisodes != 1 || result.Summary.InfraFailures != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestInfrastructureFailureMarksRunFailed(t *testing.T) {
	output := t.TempDir()
	_, err := (Service{BackendFactory: func(BackendRequest) (backend.Backend, error) {
		return failingTaskBackend{taskIDs: map[string]struct{}{"task-b": {}}}, nil
	}}).Run(Params{
		TaskSetRef: buildTaskSetMultiple(t), Agent: "oracle", Model: "test/model", RunID: "infra-failure",
		BackendName: "local", Concurrency: 1, Attempts: 1, Execute: true, Output: output,
	})
	if err == nil {
		t.Fatal("Run error = nil")
	}
	var run domain.RolloutRun
	readJSON(t, filepath.Join(output, "infra-failure", "run.json"), &run)
	if run.Status != domain.RunStatusFailed || run.Summary == nil || run.Summary.InfraFailures != 1 {
		t.Fatalf("run = %#v", run)
	}
}

func TestTaskOutputsAreCollectedAndRequired(t *testing.T) {
	bundle := buildTaskSetWithOutput(t)
	for name, command := range map[string]string{"present": "printf '{\"ok\":true}' > result.json", "missing": "true"} {
		t.Run(name, func(t *testing.T) {
			result, err := (Service{}).Run(Params{
				TaskSetRef:   bundle,
				Agent:        "command",
				AgentCommand: command,
				BackendName:  "local",
				Concurrency:  1,
				Attempts:     1,
				Execute:      true,
				Output:       t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			episodeID := domain.NewEpisodeID(result.RunID, "output-task", 1)
			var episode domain.Episode
			readJSON(t, filepath.Join(result.RunDir, "episodes", episodeID, "episode.json"), &episode)
			if name == "present" {
				if episode.Status != domain.EpisodeStatusCompleted || len(episode.Artifacts) == 0 {
					t.Fatalf("episode = %#v", episode)
				}
			} else if episode.Status != domain.EpisodeStatusFailed || episode.FailureClass != domain.FailureClassVerifierFailed {
				t.Fatalf("missing required output episode = %#v", episode)
			}
		})
	}
}

func TestSelectionIsFrozenInPlan(t *testing.T) {
	bundle := buildTaskSetMultiple(t)
	result, err := (Service{}).Run(Params{
		TaskSetRef:      bundle,
		Agent:           "oracle",
		Model:           "test/model",
		BackendName:     "local",
		Concurrency:     1,
		Attempts:        1,
		SelectedTaskIDs: []string{"task-b"},
		Output:          t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var plan domain.RolloutPlan
	readJSON(t, filepath.Join(result.RunDir, "plan.json"), &plan)
	if len(plan.TaskIDs) != 1 || plan.TaskIDs[0] != "task-b" {
		t.Fatalf("frozen task ids = %#v", plan.TaskIDs)
	}
}

func TestRejectsUnknownSelectedTask(t *testing.T) {
	_, err := (Service{}).Run(Params{
		TaskSetRef: buildTaskSetMultiple(t), Agent: "oracle", Model: "test/model",
		BackendName: "local", Concurrency: 1, Attempts: 1, SelectedTaskIDs: []string{"missing"}, Output: t.TempDir(),
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestResumeUsesCapturedPlanAfterBundleRemoval(t *testing.T) {
	bundle := buildTaskSet(t)
	result, err := (Service{}).Run(Params{
		TaskSetRef: bundle, Agent: "oracle", Model: "test/model", BackendName: "local",
		Concurrency: 1, Attempts: 1, Output: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(bundle)); err != nil {
		t.Fatal(err)
	}
	resumed, err := (Service{}).Run(Params{ResumeRunDir: result.RunDir, Execute: true, Concurrency: 1})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if !resumed.Resumed || resumed.RunID != result.RunID {
		t.Fatalf("resumed = %#v", resumed)
	}
}

func TestResumeReconcilesValidatedTerminalRunStatus(t *testing.T) {
	bundle := buildTaskSet(t)
	result, err := (Service{}).Run(Params{
		TaskSetRef: bundle, Agent: "command", AgentCommand: "true", BackendName: "local",
		Concurrency: 1, Attempts: 1, Execute: true, Output: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var run domain.RolloutRun
	readJSON(t, result.RunJSONPath, &run)
	run.Status = domain.RunStatusFailed
	run.Summary.InfraFailures = 1
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(result.RunJSONPath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	resumed, err := (Service{}).Run(Params{ResumeRunDir: result.RunDir, Execute: true, Concurrency: 1})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if resumed.Status != domain.RunStatusCompleted || resumed.Summary.InfraFailures != 0 {
		t.Fatalf("resumed = %#v, want completed run without stale infrastructure failures", resumed)
	}
	run = domain.RolloutRun{}
	readJSON(t, result.RunJSONPath, &run)
	if run.Status != domain.RunStatusCompleted || run.Summary.InfraFailures != 0 {
		t.Fatalf("persisted run = %#v, want reconciled terminal status", run)
	}
}

func TestShardSelectionIsStable(t *testing.T) {
	bundle := buildTaskSetMultiple(t)
	var selected []string
	for index := 0; index < 2; index++ {
		result, err := (Service{}).Run(Params{
			TaskSetRef: bundle, Agent: "oracle", Model: "test/model", BackendName: "local",
			Concurrency: 1, Attempts: 1, ShardIndex: index, ShardCount: 2, Output: t.TempDir(),
		})
		if err != nil {
			t.Fatal(err)
		}
		var plan domain.RolloutPlan
		readJSON(t, filepath.Join(result.RunDir, "plan.json"), &plan)
		selected = append(selected, plan.TaskIDs...)
	}
	if len(selected) != 2 || selected[0] == selected[1] {
		t.Fatalf("shards selected %#v", selected)
	}
}

func TestResumeRejectsCreateTimeOverrides(t *testing.T) {
	for name, params := range map[string]Params{
		"runner":   {ResumeRunDir: "/run", Execute: true, Concurrency: 1, BackendName: "axern"},
		"taskset":  {ResumeRunDir: "/run", Execute: true, Concurrency: 1, TaskSetRef: "replacement"},
		"attempts": {ResumeRunDir: "/run", Execute: true, Concurrency: 1, Attempts: 2},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeParams(params); err == nil {
				t.Fatal("NormalizeParams error = nil")
			}
		})
	}
}

func buildTaskSet(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "input.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata:
  name: test
spec:
  generators:
    - task_id: example
      instruction:
        text: Do the task.
      workspace:
        paths: [workspace]
        expand: aggregate
      task:
        sandbox:
          backend: local
          runtime_class: ""
          workdir: /workspace
        verifier:
          type: shell
          command: "true"
`
	buildFile := filepath.Join(root, "taskset.yaml")
	if err := os.WriteFile(buildFile, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	if _, err := taskset.Build(taskset.BuildParams{File: buildFile, Output: bundle}); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func buildTaskSetMultiple(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name, "input.txt"), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: multi}
spec:
  generators:
    - task_id: task-a
      instruction: {text: A}
      workspace: {paths: [a], expand: aggregate}
      task:
        sandbox: {backend: local, workdir: /workspace}
        verifier: {type: none}
    - task_id: task-b
      instruction: {text: B}
      workspace: {paths: [b], expand: aggregate}
      task:
        sandbox: {backend: local, workdir: /workspace}
        verifier: {type: none}
`
	buildFile := filepath.Join(root, "taskset.yaml")
	if err := os.WriteFile(buildFile, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	if _, err := taskset.Build(taskset.BuildParams{File: buildFile, Output: bundle}); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func buildTaskSetWithOutput(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: output}
spec:
  generators:
    - task_id: output-task
      instruction: {text: produce output}
      workspace: {paths: [input.txt], expand: aggregate}
      task:
        sandbox: {backend: local, workdir: /workspace}
        verifier: {type: shell, command: "true"}
        outputs:
          - path: result.json
            required: true
            json_schema: '{"type":"object"}'
`
	buildFile := filepath.Join(root, "taskset.yaml")
	if err := os.WriteFile(buildFile, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	if _, err := taskset.Build(taskset.BuildParams{File: buildFile, Output: bundle}); err != nil {
		t.Fatal(err)
	}
	return bundle
}
