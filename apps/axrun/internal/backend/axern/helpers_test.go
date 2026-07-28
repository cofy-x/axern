package axern

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentpkg "github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/agent/claudecode"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/localstore"
	"github.com/cofy-x/axern/apps/axrun/internal/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
)

func executeRequest(store localstore.Store, layout localstore.EpisodeLayout) backend.ExecuteRequest {
	return backend.ExecuteRequest{
		Store:   store,
		Task:    layout.TaskInstance,
		Episode: layout.Episode,
		Paths: rollout.Paths{
			EpisodeJSONPath:  layout.EpisodeJSONPath,
			TrajectoryPath:   layout.TrajectoryPath,
			AgentJSONPath:    layout.AgentJSONPath,
			VerifierJSONPath: layout.VerifierJSONPath,
			RewardJSONPath:   layout.RewardJSONPath,
			ArtifactDir:      layout.ArtifactDir,
		},
	}
}

func createLayout(t *testing.T, verifier domain.VerifierSpec) (localstore.Store, localstore.EpisodeLayout) {
	t.Helper()
	store := localstore.New(filepath.Join(t.TempDir(), "runs"))
	runLayout, err := store.CreateRunLayout(domain.RolloutRun{
		ID:              "test-run",
		Status:          domain.RunStatusCreated,
		CreatedAt:       fixedNow(),
		Agent:           domain.AgentSpec{Name: "oracle"},
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

func fixedNow() time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
}

func testRegistry() *agentpkg.Registry {
	r := agentpkg.NewRegistry()
	_ = r.Register(agentpkg.Registration{
		Name:     "claude-code",
		Provider: agentpkg.ProviderAnthropic,
		Kind:     agentpkg.RegistrationKindManaged,
		HarnessFactory: func(spec domain.AgentSpec) (agentpkg.Harness, error) {
			return claudecode.NewFromEnv()
		},
	})
	_ = r.Register(agentpkg.Registration{Name: "oracle", Provider: agentpkg.ProviderNone, Kind: agentpkg.RegistrationKindBuiltin})
	_ = r.Register(agentpkg.Registration{
		Name:         "noop",
		Provider:     agentpkg.ProviderNone,
		Kind:         agentpkg.RegistrationKindBuiltin,
		RunByDefault: true,
		HarnessFactory: func(spec domain.AgentSpec) (agentpkg.Harness, error) {
			return agentpkg.NoopHarness{}, nil
		},
	})
	return r
}

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

type fakeRuntime struct {
	sandbox  *fakeSandbox
	instance sandbox.Instance
	err      error
}

func (f *fakeRuntime) Preflight() error {
	return f.err
}

func (f *fakeRuntime) Create(context.Context) (sandbox.Instance, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.instance != nil {
		return f.instance, nil
	}
	return f.sandbox, nil
}

type fakeSandbox struct {
	closeErr           error
	execResult         sandbox.ExecResult
	execErr            error
	execCalls          int
	execCommand        sandbox.ExecCommand
	execOptions        sandbox.ExecOptions
	execCallsList      []fakeExecCall
	uploadLocalPath    string
	uploadRemotePath   string
	downloadRemotePath string
	downloadLocalPath  string
}

type fakeExecCall struct {
	command sandbox.ExecCommand
	options sandbox.ExecOptions
}

func (f *fakeSandbox) Close(context.Context) error {
	return f.closeErr
}

func (f *fakeSandbox) Exec(_ context.Context, command sandbox.ExecCommand, options sandbox.ExecOptions) (sandbox.ExecResult, error) {
	f.execCalls++
	f.execCommand = command
	f.execOptions = options
	f.execCallsList = append(f.execCallsList, fakeExecCall{command: command, options: options})
	return f.execResult, f.execErr
}

func (f *fakeSandbox) UploadDir(_ context.Context, localPath string, remotePath string, _ sandbox.UploadDirOptions) error {
	f.uploadLocalPath = localPath
	f.uploadRemotePath = remotePath
	return nil
}

func (f *fakeSandbox) DownloadPath(_ context.Context, remotePath string, localPath string, _ sandbox.DownloadPathOptions) error {
	f.downloadRemotePath = remotePath
	f.downloadLocalPath = localPath
	return os.MkdirAll(localPath, 0o755)
}

func (f *fakeSandbox) State() (sandbox.State, error) {
	return sandbox.State{AllocationID: "alloc-1"}, nil
}
