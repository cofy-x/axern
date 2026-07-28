package localstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestCreateRunLayoutWritesLayoutAndJSON(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), ".axrun", "runs"))
	run := testRun("test-run")

	result, err := store.CreateRunLayout(run)
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}

	for _, path := range []string{result.RunDir, result.InputsDir, result.TasksDir, result.EpisodesDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", path)
		}
	}
	data, err := os.ReadFile(result.RunJSONPath)
	if err != nil {
		t.Fatalf("read run.json: %v", err)
	}
	var decoded domain.RolloutRun
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode run.json: %v", err)
	}
	if decoded.ID != "test-run" || decoded.OutputPath != "." {
		t.Fatalf("decoded run = %#v", decoded)
	}
	if decoded.SchemaVersion != domain.LocalSchemaVersion {
		t.Fatalf("schema version = %q", decoded.SchemaVersion)
	}
	if result.PlanJSONPath != filepath.Join(result.RunDir, "plan.json") {
		t.Fatalf("plan path = %q", result.PlanJSONPath)
	}
}

func TestCreateRunLayoutRejectsExistingRunDir(t *testing.T) {
	store := New(t.TempDir())
	run := testRun("test-run")
	if _, err := store.CreateRunLayout(run); err != nil {
		t.Fatalf("CreateRunLayout first returned error: %v", err)
	}
	if _, err := store.CreateRunLayout(run); err == nil {
		t.Fatal("CreateRunLayout second error = nil, want existing run error")
	}
}

func TestCreateRunLayoutRejectsPathLikeRunID(t *testing.T) {
	store := New(t.TempDir())
	for _, id := range []string{"../escape", "nested/run", `nested\run`, ".", ".."} {
		run := testRun(id)
		if _, err := store.CreateRunLayout(run); err == nil {
			t.Fatalf("CreateRunLayout(%q) error = nil, want path segment error", id)
		}
	}
}

func TestWriteRolloutRunUpdatesRunJSON(t *testing.T) {
	store := New(t.TempDir())
	result, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	run := result.RolloutRun
	run.Status = domain.RunStatusCompleted
	if err := store.WriteRolloutRun(result.RunJSONPath, run); err != nil {
		t.Fatalf("WriteRolloutRun returned error: %v", err)
	}
	var decoded domain.RolloutRun
	if err := readJSON(result.RunJSONPath, &decoded); err != nil {
		t.Fatalf("decode run.json: %v", err)
	}
	if decoded.Status != domain.RunStatusCompleted {
		t.Fatalf("decoded run = %#v", decoded)
	}
}

func TestWriteRolloutPlanWritesPlanJSON(t *testing.T) {
	store := New(t.TempDir())
	result, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	plan := domain.RolloutPlan{
		SchemaVersion:   domain.LocalSchemaVersion,
		RunID:           "test-run",
		CreatedAt:       result.RolloutRun.CreatedAt,
		Selection:       domain.TaskSelection{ResolvedTaskCount: 1, SelectedTaskCount: 1},
		Concurrency:     1,
		AttemptsPerTask: 1,
		TaskIDs:         []string{"smoke-task"},
		Episodes: []domain.PlannedEpisode{{
			ID:           domain.NewEpisodeID("test-run", "smoke-task", 1),
			TaskID:       "smoke-task",
			AttemptIndex: 1,
			Order:        1,
		}},
	}
	if err := store.WriteRolloutPlan(result.PlanJSONPath, plan); err != nil {
		t.Fatalf("WriteRolloutPlan returned error: %v", err)
	}
	var decoded domain.RolloutPlan
	if err := readJSON(result.PlanJSONPath, &decoded); err != nil {
		t.Fatalf("decode plan.json: %v", err)
	}
	if decoded.RunID != "test-run" || len(decoded.Episodes) != 1 {
		t.Fatalf("decoded plan = %#v", decoded)
	}
}

func TestLoadRunReturnsPlanOrderedEpisodeLayouts(t *testing.T) {
	store := New(t.TempDir())
	run := testRun("test-run")
	run.TaskIDs = []string{"task-a", "task-b"}
	run.Summary = &domain.RunSummary{TaskCount: 2, EpisodeCount: 2, PendingEpisodes: 2}
	layout, err := store.CreateRunLayout(run)
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	taskA := domain.TaskInstance{ID: "task-a", Instruction: "A", Sandbox: run.Sandbox, Verifier: domain.VerifierSpec{Type: domain.VerifierTypeNone}}
	taskB := domain.TaskInstance{ID: "task-b", Instruction: "B", Sandbox: run.Sandbox, Verifier: domain.VerifierSpec{Type: domain.VerifierTypeNone}}
	episodeA := domain.Episode{
		ID:           domain.NewEpisodeID(run.ID, taskA.ID, 1),
		RunID:        run.ID,
		TaskID:       taskA.ID,
		AttemptIndex: 1,
		Status:       domain.EpisodeStatusPending,
		Agent:        run.Agent,
		Model:        run.Model,
		Sandbox:      run.Sandbox,
	}
	episodeB := domain.Episode{
		ID:           domain.NewEpisodeID(run.ID, taskB.ID, 1),
		RunID:        run.ID,
		TaskID:       taskB.ID,
		AttemptIndex: 1,
		Status:       domain.EpisodeStatusPending,
		Agent:        run.Agent,
		Model:        run.Model,
		Sandbox:      run.Sandbox,
	}
	if _, err := store.CreateEpisodeLayout(layout, taskB, episodeB); err != nil {
		t.Fatalf("CreateEpisodeLayout B returned error: %v", err)
	}
	if _, err := store.CreateEpisodeLayout(layout, taskA, episodeA); err != nil {
		t.Fatalf("CreateEpisodeLayout A returned error: %v", err)
	}
	plan := domain.RolloutPlan{
		SchemaVersion:   domain.LocalSchemaVersion,
		RunID:           run.ID,
		CreatedAt:       run.CreatedAt,
		Selection:       domain.TaskSelection{ResolvedTaskCount: 2, SelectedTaskCount: 2},
		Concurrency:     run.Concurrency,
		AttemptsPerTask: run.AttemptsPerTask,
		TaskIDs:         run.TaskIDs,
		Episodes: []domain.PlannedEpisode{
			{ID: episodeA.ID, TaskID: taskA.ID, AttemptIndex: 1, Order: 1},
			{ID: episodeB.ID, TaskID: taskB.ID, AttemptIndex: 1, Order: 2},
		},
	}
	if err := store.WriteRolloutPlan(layout.PlanJSONPath, plan); err != nil {
		t.Fatalf("WriteRolloutPlan returned error: %v", err)
	}
	loaded, err := LoadRun(layout.RunDir)
	if err != nil {
		t.Fatalf("LoadRun returned error: %v", err)
	}
	if loaded.Layout.RunDir != layout.RunDir || loaded.Plan.RunID != run.ID {
		t.Fatalf("loaded = %#v", loaded)
	}
	if len(loaded.Episodes) != 2 ||
		loaded.Episodes[0].Episode.ID != episodeA.ID ||
		loaded.Episodes[1].Episode.ID != episodeB.ID {
		t.Fatalf("loaded episodes = %#v", loaded.Episodes)
	}
}

func TestLoadRunRejectsEmptyRunDir(t *testing.T) {
	if _, err := LoadRun(" "); err == nil {
		t.Fatal("LoadRun error = nil, want empty run dir error")
	}
}

func TestLoadRunRejectsPathLikePlannedIDsBeforeReadingRecords(t *testing.T) {
	store := New(t.TempDir())
	run := testRun("test-run")
	run.TaskIDs = []string{"../escape"}
	layout, err := store.CreateRunLayout(run)
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	plan := domain.RolloutPlan{
		SchemaVersion:   domain.LocalSchemaVersion,
		RunID:           run.ID,
		CreatedAt:       run.CreatedAt,
		Selection:       domain.TaskSelection{ResolvedTaskCount: 1, SelectedTaskCount: 1},
		Concurrency:     run.Concurrency,
		AttemptsPerTask: run.AttemptsPerTask,
		TaskIDs:         run.TaskIDs,
		Episodes: []domain.PlannedEpisode{{
			ID:           "episode-test-run-escape-1",
			TaskID:       "../escape",
			AttemptIndex: 1,
			Order:        1,
		}},
	}
	if err := writeJSON(layout.PlanJSONPath, plan); err != nil {
		t.Fatalf("write plan.json: %v", err)
	}

	_, err = LoadRun(layout.RunDir)
	if err == nil {
		t.Fatal("LoadRun error = nil, want path segment error")
	}
	if !strings.Contains(err.Error(), "planned task id") {
		t.Fatalf("LoadRun error = %v, want planned task id error", err)
	}
}

func TestLoadRunRejectsMissingPlanJSON(t *testing.T) {
	store := New(t.TempDir())
	layout, err := store.CreateRunLayout(testRun("test-run"))
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	plan := domain.RolloutPlan{
		SchemaVersion:   domain.LocalSchemaVersion,
		RunID:           layout.RolloutRun.ID,
		CreatedAt:       layout.RolloutRun.CreatedAt,
		Selection:       domain.TaskSelection{ResolvedTaskCount: 0, SelectedTaskCount: 0},
		Concurrency:     layout.RolloutRun.Concurrency,
		AttemptsPerTask: layout.RolloutRun.AttemptsPerTask,
	}
	if err := store.WriteRolloutPlan(layout.PlanJSONPath, plan); err != nil {
		t.Fatalf("WriteRolloutPlan returned error: %v", err)
	}
	if err := os.Remove(layout.PlanJSONPath); err != nil {
		t.Fatalf("remove plan.json: %v", err)
	}
	_, err = LoadRun(layout.RunDir)
	if err == nil {
		t.Fatal("LoadRun error = nil, want missing plan.json error")
	}
	if !strings.Contains(err.Error(), "read plan.json") {
		t.Fatalf("LoadRun error = %v, want read plan.json error", err)
	}
}

func TestLoadRunRejectsInvalidEpisodeJSON(t *testing.T) {
	store := New(t.TempDir())
	run := testRun("test-run")
	run.TaskIDs = []string{"task-a"}
	layout, err := store.CreateRunLayout(run)
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	task := testTask("task-a")
	episode := testEpisode(run.ID, task.ID)
	episodeLayout, err := store.CreateEpisodeLayout(layout, task, episode)
	if err != nil {
		t.Fatalf("CreateEpisodeLayout returned error: %v", err)
	}
	plan := domain.RolloutPlan{
		SchemaVersion:   domain.LocalSchemaVersion,
		RunID:           run.ID,
		CreatedAt:       run.CreatedAt,
		Selection:       domain.TaskSelection{ResolvedTaskCount: 1, SelectedTaskCount: 1},
		Concurrency:     run.Concurrency,
		AttemptsPerTask: run.AttemptsPerTask,
		TaskIDs:         run.TaskIDs,
		Episodes: []domain.PlannedEpisode{
			{ID: episode.ID, TaskID: task.ID, AttemptIndex: 1, Order: 1},
		},
	}
	if err := store.WriteRolloutPlan(layout.PlanJSONPath, plan); err != nil {
		t.Fatalf("WriteRolloutPlan returned error: %v", err)
	}
	if err := os.WriteFile(episodeLayout.EpisodeJSONPath, []byte("{bad json\n"), 0o644); err != nil {
		t.Fatalf("write invalid episode json: %v", err)
	}
	_, err = LoadRun(layout.RunDir)
	if err == nil {
		t.Fatal("LoadRun error = nil, want invalid episode json error")
	}
	if !strings.Contains(err.Error(), "read episode") {
		t.Fatalf("LoadRun error = %v, want read episode error", err)
	}
}
