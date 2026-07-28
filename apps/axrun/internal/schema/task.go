package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

type taskIndex map[string]string

func (index taskIndex) count() int {
	return len(index)
}

func validateTasks(problems *collector, runDir string, run domain.RolloutRun) taskIndex {
	tasksDir := filepath.Join(runDir, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		problems.add(displayPath(runDir, tasksDir), "", fmt.Sprintf("read tasks directory: %v", err))
		return nil
	}
	seen := taskIndex{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		taskPath := filepath.Join(tasksDir, entry.Name(), "task.json")
		task, ok := readJSON[domain.TaskInstance](problems, runDir, taskPath)
		if !ok {
			continue
		}
		seen[task.ID] = taskPath
		validateTaskRecord(problems, runDir, taskPath, task)
	}
	listed := map[string]struct{}{}
	for _, taskID := range run.TaskIDs {
		listed[taskID] = struct{}{}
		if _, ok := seen[taskID]; !ok {
			problems.add(displayPath(runDir, filepath.Join(tasksDir, taskID, "task.json")), "task_ids", "listed task is missing")
		}
	}
	for _, taskID := range sortedTaskIDs(seen) {
		if _, ok := listed[taskID]; !ok {
			problems.add(displayPath(runDir, seen[taskID]), "id", "task is missing from run.task_ids")
		}
	}
	return seen
}

func validateTaskRecord(problems *collector, runDir string, path string, task domain.TaskInstance) {
	rel := displayPath(runDir, path)
	problems.required(rel, "id", task.ID)
	validatePathSegment(problems, rel, "id", task.ID)
	problems.required(rel, "instruction", task.Instruction)
	validateSourceRef(problems, runDir, rel, "source", task.Source)
	validateSandboxSpec(problems, rel, "sandbox", task.Sandbox)
	validateSandboxRuntimeSourceRefs(problems, runDir, rel, task.Sandbox.RuntimeSource)
	validateVerifierSpec(problems, rel, "verifier", task.Verifier)
	taskSetTaskID := ""
	if task.InitialState != nil && task.InitialState.WorkspaceImage != nil {
		taskSetTaskID = task.ID
	}
	validateVerifierAssets(problems, runDir, rel, task.Verifier.Assets, taskSetTaskID)
	validateInitialStateSpec(problems, runDir, rel, "initial_state", task.InitialState)
	validateOracleSpec(problems, runDir, rel, task.Oracle, taskSetTaskID)
}

func sortedTaskIDs(tasks taskIndex) []string {
	ids := make([]string, 0, len(tasks))
	for id := range tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
