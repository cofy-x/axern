package rundetail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/application/agentcatalog"
	"github.com/cofy-x/axern/apps/axrun/internal/application/resumepolicy"
	rolloutapp "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

func TestLoadReturnsEpisodeResumeAndManifestSummary(t *testing.T) {
	created, err := (rolloutapp.Service{AgentRegistry: agentcatalog.DefaultRegistry()}).Run(rolloutapp.Params{
		TaskSetRef: buildTaskSet(t), Agent: "oracle", Model: "m", RunID: "test-run", BackendName: "local", Concurrency: 1, Attempts: 1, Output: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	detail, err := Load(created.RunDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if detail.RunID != "test-run" || len(detail.Episodes) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	episode := detail.Episodes[0]
	if episode.Resume.Action != resumepolicy.ActionExecute || episode.Resume.Reason != resumepolicy.ReasonPending {
		t.Fatalf("episode resume = %#v", episode.Resume)
	}
	if detail.Resume.ExecutableEpisodes != 1 || detail.Resume.SkippedEpisodes != 0 {
		t.Fatalf("resume summary = %#v", detail.Resume)
	}
}

func TestLoadRejectsEscapingArtifactManifestSummaryPath(t *testing.T) {
	created, err := (rolloutapp.Service{AgentRegistry: agentcatalog.DefaultRegistry()}).Run(rolloutapp.Params{
		TaskSetRef: buildTaskSet(t), Agent: "oracle", Model: "m", RunID: "test-run", BackendName: "local", Concurrency: 1, Attempts: 1, Output: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	episodeID := domain.NewEpisodeID("test-run", "task-1", 1)
	episodePath := filepath.Join(created.RunDir, "episodes", episodeID, "episode.json")
	data, err := os.ReadFile(episodePath)
	if err != nil {
		t.Fatalf("read episode: %v", err)
	}
	var episode domain.Episode
	if err := json.Unmarshal(data, &episode); err != nil {
		t.Fatalf("decode episode: %v", err)
	}
	episode.ArtifactManifestPath = "../outside.json"
	updated, err := json.MarshalIndent(episode, "", "  ")
	if err != nil {
		t.Fatalf("encode episode: %v", err)
	}
	if err := os.WriteFile(episodePath, append(updated, '\n'), 0o644); err != nil {
		t.Fatalf("write episode: %v", err)
	}

	detail, err := Load(created.RunDir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(detail.Episodes) != 1 || !strings.Contains(detail.Episodes[0].ArtifactManifest.Error, "run-root-relative") {
		t.Fatalf("episode detail = %#v", detail.Episodes)
	}
}

func buildTaskSet(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: detail-test}
spec:
  generators:
    - task_id: task-1
      instruction: {text: hello}
      workspace: {paths: [workspace], expand: aggregate}
      task:
        sandbox: {backend: local, runtime_class: "", workdir: /workspace}
        verifier: {type: shell, command: "true"}
`
	file := filepath.Join(root, "taskset.yaml")
	if err := os.WriteFile(file, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	if _, err := taskset.Build(taskset.BuildParams{File: file, Output: bundle}); err != nil {
		t.Fatal(err)
	}
	return bundle
}
