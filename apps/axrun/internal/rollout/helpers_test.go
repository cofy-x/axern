package rollout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

type recordingAgent struct {
	called  bool
	request agent.Request
	result  agent.Result
}

func (r *recordingAgent) Preflight() error {
	return nil
}

func (r *recordingAgent) Run(_ context.Context, request agent.Request) (agent.Result, error) {
	r.called = true
	r.request = request
	if r.result.Status != "" {
		return r.result, nil
	}
	return agent.Result{Status: domain.AgentStatusCompleted, Summary: "agent done"}, nil
}

type fakeRuntime struct {
	sandbox sandbox.Instance
}

func (f fakeRuntime) Preflight() error {
	return nil
}

func (f fakeRuntime) Create(context.Context) (sandbox.Instance, error) {
	return f.sandbox, nil
}

type fakeSandbox struct {
	execCalls          int
	execResult         sandbox.ExecResult
	execErr            error
	execHook           func(sandbox.ExecCommand, sandbox.ExecOptions) (sandbox.ExecResult, error)
	closeErr           error
	uploadLocalPath    string
	uploadRemotePath   string
	uploadLocalPaths   []string
	uploadRemotePaths  []string
	uploadOptions      []sandbox.UploadDirOptions
	uploadHook         func(string)
	downloadRemotePath string
	downloadLocalPath  string
	downloadHook       func(string, string) error
	downloadErr        error
}

func (f *fakeSandbox) Exec(_ context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	f.execCalls++
	if f.execHook != nil {
		return f.execHook(command, options)
	}
	return f.execResult, f.execErr
}

func (f *fakeSandbox) UploadDir(_ context.Context, localPath string, remotePath string, options sandbox.UploadDirOptions) error {
	f.uploadLocalPath = localPath
	f.uploadRemotePath = remotePath
	f.uploadLocalPaths = append(f.uploadLocalPaths, localPath)
	f.uploadRemotePaths = append(f.uploadRemotePaths, remotePath)
	f.uploadOptions = append(f.uploadOptions, options)
	if f.uploadHook != nil {
		f.uploadHook(localPath)
	}
	return nil
}

func (f *fakeSandbox) DownloadPath(_ context.Context, remotePath string, localPath string, _ sandbox.DownloadPathOptions) error {
	f.downloadRemotePath = remotePath
	f.downloadLocalPath = localPath
	if f.downloadErr != nil {
		return f.downloadErr
	}
	if f.downloadHook != nil {
		return f.downloadHook(remotePath, localPath)
	}
	return os.MkdirAll(localPath, 0o755)
}

func (f *fakeSandbox) State() (sandbox.State, error) {
	return sandbox.State{AllocationID: "alloc-1"}, nil
}

func (f *fakeSandbox) Close(context.Context) error {
	return f.closeErr
}

func createLayout(t *testing.T, verifier domain.VerifierSpec) (localstore.Store, localstore.EpisodeLayout) {
	t.Helper()
	store := localstore.New(filepath.Join(t.TempDir(), "runs"))
	runLayout, err := store.CreateRunLayout(domain.RolloutRun{
		ID:              "test-run",
		Status:          domain.RunStatusCreated,
		CreatedAt:       fixedNow(),
		Agent:           domain.AgentSpec{Name: "claude-code"},
		Model:           domain.ModelSpec{ID: "anthropic/claude-haiku-4-5"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		AttemptsPerTask: 1,
	})
	if err != nil {
		t.Fatalf("CreateRunLayout returned error: %v", err)
	}
	task := domain.TaskInstance{
		ID:          "smoke-task",
		Instruction: "Print hello",
		Sandbox:     runLayout.RolloutRun.Sandbox,
		Verifier:    verifier,
		Tags:        []string{},
	}
	episode := domain.Episode{
		ID:           domain.NewEpisodeID(runLayout.RolloutRun.ID, task.ID, 1),
		RunID:        runLayout.RolloutRun.ID,
		TaskID:       task.ID,
		AttemptIndex: 1,
		Status:       domain.EpisodeStatusPending,
		Agent:        runLayout.RolloutRun.Agent,
		Model:        runLayout.RolloutRun.Model,
		Sandbox:      runLayout.RolloutRun.Sandbox,
	}
	layout, err := store.CreateEpisodeLayout(runLayout, task, episode)
	if err != nil {
		t.Fatalf("CreateEpisodeLayout returned error: %v", err)
	}
	return store, layout
}

func paths(layout localstore.EpisodeLayout) Paths {
	return Paths{
		EpisodeJSONPath:  layout.EpisodeJSONPath,
		TrajectoryPath:   layout.TrajectoryPath,
		AgentJSONPath:    layout.AgentJSONPath,
		VerifierJSONPath: layout.VerifierJSONPath,
		RewardJSONPath:   layout.RewardJSONPath,
		ArtifactDir:      layout.ArtifactDir,
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
}

func readTrajectorySteps(t *testing.T, path string) []domain.TrajectoryStep {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trajectory: %v", err)
	}
	defer file.Close()
	var steps []domain.TrajectoryStep
	decoder := json.NewDecoder(file)
	for decoder.More() {
		var step domain.TrajectoryStep
		if err := decoder.Decode(&step); err != nil {
			t.Fatalf("decode trajectory: %v", err)
		}
		steps = append(steps, step)
	}
	return steps
}

func readJSON(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
