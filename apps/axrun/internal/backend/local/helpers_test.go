package local

import (
	"bufio"
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

func fixedNow() time.Time {
	return time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
}

func testRegistry() *agentpkg.Registry {
	r := agentpkg.NewRegistry()
	_ = r.Register(agentpkg.Registration{
		Name:              "command",
		Provider:          agentpkg.ProviderNone,
		SupportedRuntimes: []domain.AgentRuntimeType{domain.AgentRuntimeTypeSandboxCommand},
		Kind:              agentpkg.RegistrationKindCommand,
		HarnessFactory: func(spec domain.AgentSpec) (agentpkg.Harness, error) {
			return agentpkg.CommandHarness{}, nil
		},
	})
	_ = r.Register(agentpkg.Registration{
		Name:     "claude-code",
		Provider: agentpkg.ProviderAnthropic,
		Kind:     agentpkg.RegistrationKindManaged,
		HarnessFactory: func(spec domain.AgentSpec) (agentpkg.Harness, error) {
			return claudecode.NewFromEnv()
		},
	})
	_ = r.Register(agentpkg.Registration{
		Name:               "oracle",
		Provider:           agentpkg.ProviderNone,
		Kind:               agentpkg.RegistrationKindBuiltin,
		DefaultRuntimeType: domain.AgentRuntimeTypeOracle,
		RunByDefault:       true,
		HarnessFactory: func(spec domain.AgentSpec) (agentpkg.Harness, error) {
			return agentpkg.NoopHarness{}, nil
		},
	})
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

func countTrajectorySteps(t *testing.T, path string) int {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trajectory: %v", err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trajectory: %v", err)
	}
	return count
}

func readTrajectorySteps(t *testing.T, path string) []domain.TrajectoryStep {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open trajectory: %v", err)
	}
	defer file.Close()
	var steps []domain.TrajectoryStep
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var step domain.TrajectoryStep
		if err := json.Unmarshal(scanner.Bytes(), &step); err != nil {
			t.Fatalf("decode trajectory step: %v", err)
		}
		steps = append(steps, step)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan trajectory: %v", err)
	}
	return steps
}
