package localstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestWriteAgentResultPreservesLogAndTelemetryRefs(t *testing.T) {
	store, result := createEpisodeLayout(t)
	exitCode := 0
	agentResult := domain.AgentResult{
		Status:    domain.AgentStatusCompleted,
		ExitCode:  &exitCode,
		StdoutRef: "artifacts/agent.stdout",
		StderrRef: "artifacts/agent.stderr",
		RawLogRef: "artifacts/agent.raw.jsonl",
		Usage:     &domain.UsageMetrics{InputTokens: 10, OutputTokens: 5, TotalTokens: 15, ToolCalls: 2},
		Cost:      &domain.CostMetrics{Amount: 0.01, Currency: "USD"},
		Artifacts: []domain.ArtifactRef{{Path: "artifacts/agent.patch", Kind: "patch"}},
	}
	if err := store.WriteAgentResult(result.AgentJSONPath, agentResult); err != nil {
		t.Fatalf("WriteAgentResult returned error: %v", err)
	}
	var decoded domain.AgentResult
	if err := readJSON(result.AgentJSONPath, &decoded); err != nil {
		t.Fatalf("decode agent.json: %v", err)
	}
	if decoded.StdoutRef != agentResult.StdoutRef ||
		decoded.RawLogRef != agentResult.RawLogRef ||
		decoded.Usage.TotalTokens != 15 ||
		decoded.Cost.Currency != "USD" ||
		len(decoded.Artifacts) != 1 {
		t.Fatalf("agent result = %#v", decoded)
	}
}

func TestAppendAgentRawEventAppendsJSONL(t *testing.T) {
	store, result := createEpisodeLayout(t)
	ref, err := store.AppendAgentRawEvent(result.ArtifactDir, domain.AgentRawEvent{
		Type:         domain.AgentRawEventArtifact,
		ArtifactRef:  "episodes/episode_test-run_smoke-task_1/artifacts/agent.stdout.txt",
		ArtifactKind: domain.ArtifactKindAgentStdout,
	})
	if err != nil {
		t.Fatalf("AppendAgentRawEvent returned error: %v", err)
	}
	if ref != "episodes/episode_test-run_smoke-task_1/artifacts/agent.raw.jsonl" {
		t.Fatalf("raw log ref = %q", ref)
	}
	data, err := os.ReadFile(filepath.Join(result.ArtifactDir, "agent.raw.jsonl"))
	if err != nil {
		t.Fatalf("read raw log: %v", err)
	}
	var event domain.AgentRawEvent
	if err := json.Unmarshal(data[:len(data)-1], &event); err != nil {
		t.Fatalf("decode raw event: %v", err)
	}
	if event.Type != domain.AgentRawEventArtifact || event.ArtifactKind != domain.ArtifactKindAgentStdout {
		t.Fatalf("event = %#v", event)
	}
	if event.EventID != "raw-000001" {
		t.Fatalf("event id = %q", event.EventID)
	}
}

func TestAppendAgentRawEventRejectsUnknownType(t *testing.T) {
	store, result := createEpisodeLayout(t)
	if _, err := store.AppendAgentRawEvent(result.ArtifactDir, domain.AgentRawEvent{Type: "bad.event"}); err == nil {
		t.Fatal("AppendAgentRawEvent error = nil")
	}
}

func TestAppendAgentRawEventPreservesExplicitEventID(t *testing.T) {
	store, result := createEpisodeLayout(t)
	if _, err := store.AppendAgentRawEvent(result.ArtifactDir, domain.AgentRawEvent{
		EventID: "custom",
		Type:    domain.AgentRawEventCommandStarted,
	}); err != nil {
		t.Fatalf("AppendAgentRawEvent returned error: %v", err)
	}
	if _, err := store.AppendAgentRawEvent(result.ArtifactDir, domain.AgentRawEvent{Type: domain.AgentRawEventCommandFinished}); err != nil {
		t.Fatalf("AppendAgentRawEvent returned error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(result.ArtifactDir, "agent.raw.jsonl"))
	if err != nil {
		t.Fatalf("read raw log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	var first, second domain.AgentRawEvent
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("decode first event: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("decode second event: %v", err)
	}
	if first.EventID != "custom" || second.EventID != "raw-000002" {
		t.Fatalf("event ids = %q, %q", first.EventID, second.EventID)
	}
}

func TestWriteAgentArtifactReturnsRunRelativePath(t *testing.T) {
	store, result := createEpisodeLayout(t)
	path, err := store.WriteAgentArtifact(result.ArtifactDir, "agent.stdout.txt", "hello")
	if err != nil {
		t.Fatalf("WriteAgentArtifact returned error: %v", err)
	}
	if path != "episodes/episode_test-run_smoke-task_1/artifacts/agent.stdout.txt" {
		t.Fatalf("artifact path = %q", path)
	}
	data, err := os.ReadFile(filepath.Join(result.ArtifactDir, "agent.stdout.txt"))
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("artifact content = %q", data)
	}
}
