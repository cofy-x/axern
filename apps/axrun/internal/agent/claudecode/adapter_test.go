package claudecode

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/proxy"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func TestClaudeCodeHarnessWithoutProfileDoesNotCreateLLMTelemetry(t *testing.T) {
	artifactDir := filepath.Join(t.TempDir(), "runs", "run-1", "episodes", "episode-1", "artifacts")
	recorder, err := proxy.NewRecorder(artifactDir, nil)
	if err != nil {
		t.Fatalf("create recorder: %v", err)
	}
	sb := &fakeSandbox{result: sandbox.ExecResult{Stdout: "done"}}
	result, err := New(Config{}).Run(context.Background(), agent.Request{
		Agent:       domain.AgentSpec{Name: "claude-code"},
		Model:       domain.ModelSpec{ID: "model"},
		Task:        domain.TaskInstance{ID: "task-1"},
		Sandbox:     sb,
		Instruction: "Do it",
		ArtifactDir: artifactDir,
		Recorder:    recorder,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.RawLogRef != "" {
		t.Fatalf("harness should not merge recorder results directly: %#v", result)
	}
	if err := proxy.FinalizeAgentResult(recorder, &result); err != nil {
		t.Fatalf("FinalizeAgentResult returned error: %v", err)
	}
	if result.RawLogRef == "" || result.LLMRequestCount != 0 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := os.Stat(filepath.Join(artifactDir, "llm")); !os.IsNotExist(err) {
		t.Fatalf("llm telemetry directory exists or stat failed: %v", err)
	}
	events := readRawEvents(t, artifactDir)
	started := assertEventExists(t, events, string(domain.AgentRawEventCommandStarted))
	if started["launcher_kind"] != string(domain.AgentLauncherKindSandboxCommand) {
		t.Fatalf("command started launcher metadata = %#v", started)
	}
	finished := assertEventExists(t, events, string(domain.AgentRawEventCommandFinished))
	if finished["launcher_kind"] != string(domain.AgentLauncherKindSandboxCommand) {
		t.Fatalf("command finished launcher metadata = %#v", finished)
	}
}

func readRawEvents(t *testing.T, artifactDir string) []map[string]any {
	t.Helper()
	file, err := os.Open(filepath.Join(artifactDir, "agent.raw.jsonl"))
	if err != nil {
		t.Fatalf("open raw log: %v", err)
	}
	defer file.Close()
	var events []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode event %q: %v", scanner.Text(), err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan raw log: %v", err)
	}
	return events
}

func assertEventExists(t *testing.T, events []map[string]any, eventType string) map[string]any {
	t.Helper()
	for _, event := range events {
		if event["type"] == eventType {
			return event
		}
	}
	t.Fatalf("missing event type %q in %#v", eventType, events)
	return nil
}
