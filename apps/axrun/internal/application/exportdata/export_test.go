package exportdata

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestExportSFTWritesRunRecordsWithRefs(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "exports", "sft.jsonl")

	result, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatSFT})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if result.RecordCount != 1 || result.RunID != "test-run" || result.OutputPath != output {
		t.Fatalf("result = %#v", result)
	}
	records := readExportJSONLines[SFTRecord](t, output)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d", len(records))
	}
	record := records[0]
	if record.SchemaVersion != sftSchemaVersion ||
		record.RecordID == "" ||
		record.SourceSchemaVersion != domain.LocalSchemaVersion ||
		record.AttemptIndex != 1 ||
		record.Agent.Name != "claude-code" ||
		record.Agent.Runtime == nil ||
		record.Agent.Runtime.Type != domain.AgentRuntimeTypeAgentImage ||
		record.Agent.Runtime.Image != "axern/claude-code-bundle:dev" ||
		record.Agent.Runtime.MountTarget != "/opt/axern/agents/claude-code" ||
		record.Agent.Runtime.BinDir != "/opt/axern/agents/claude-code/bin" ||
		record.Agent.Runtime.Prompt == nil ||
		!record.Agent.Runtime.Prompt.HasInline ||
		record.Instruction != "Reply with ok" ||
		record.Assistant != "ok\n" ||
		record.Reward.Status != domain.RewardStatusScored ||
		record.Cost == nil ||
		record.Refs.RunDir == "" ||
		record.Refs.ArtifactManifestPath == "" ||
		record.Refs.RawLogRef == "" ||
		record.Refs.LLMTelemetryRef == "" {
		t.Fatalf("record = %#v", record)
	}
}

func TestExportAgentSummaryOmitsExecutionSecrets(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "reward.jsonl")

	if _, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatReward}); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"agent.sh",
		"PROVIDER_API_KEY",
		"sk-test-secret",
		"--api-key",
		"session-secret",
		"inline secret prompt",
		`"command"`,
		`"env"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("export contains forbidden token %q: %s", forbidden, text)
		}
	}
	records := readExportJSONLines[RewardRecord](t, output)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d", len(records))
	}
	agent := records[0].Agent
	if agent.Name != "claude-code" || agent.Runtime == nil || agent.Runtime.Type != domain.AgentRuntimeTypeAgentImage {
		t.Fatalf("agent summary = %#v", agent)
	}
	if agent.Runtime.Image != "axern/claude-code-bundle:dev" ||
		agent.Runtime.MountTarget != "/opt/axern/agents/claude-code" ||
		agent.Runtime.BinDir != "/opt/axern/agents/claude-code/bin" {
		t.Fatalf("agent runtime summary = %#v", agent.Runtime)
	}
	if agent.Runtime.Session == nil || !agent.Runtime.Session.HasSessionID {
		t.Fatalf("session summary = %#v", agent.Runtime.Session)
	}
	if agent.Runtime.Prompt == nil || !agent.Runtime.Prompt.HasInline || agent.Runtime.Prompt.Rounds[0].HasSessionID != true {
		t.Fatalf("prompt summary = %#v", agent.Runtime.Prompt)
	}
}

func TestExportRewardWritesRewardAndVerifierData(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "reward.jsonl")

	if _, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatReward}); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	records := readExportJSONLines[RewardRecord](t, output)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d", len(records))
	}
	record := records[0]
	if record.SchemaVersion != rewardSchemaVersion ||
		record.RecordID == "" ||
		record.SourceSchemaVersion != domain.LocalSchemaVersion ||
		record.AttemptIndex != 1 ||
		record.Verifier.Type != "shell" ||
		record.Verifier.ExitCode == nil ||
		*record.Verifier.ExitCode != 0 ||
		record.Reward.Score == nil ||
		*record.Reward.Score != 1 ||
		record.Refs.TrajectoryPath == "" {
		t.Fatalf("record = %#v", record)
	}
}

func TestExportDefaultsOutputInsideRunDirectory(t *testing.T) {
	runDir := createExportFixture(t)

	result, err := Export(Params{RunDir: runDir, Format: FormatSFT})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	expected := filepath.Join(runDir, "exports", "sft.jsonl")
	if result.OutputPath != expected {
		t.Fatalf("output = %q, want %q", result.OutputPath, expected)
	}
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("stat default output: %v", err)
	}
	record := readExportJSONLines[SFTRecord](t, expected)[0]
	if record.Refs.RunDir != ".." {
		t.Fatalf("run_dir ref = %q, want ..", record.Refs.RunDir)
	}
}

func TestExportTraceMergesTrajectoryAndRawEvents(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "trace.jsonl")

	if _, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatTrace}); err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	records := readExportJSONLines[TraceRecord](t, output)
	if len(records) != 3 {
		t.Fatalf("len(records) = %d, records = %#v", len(records), records)
	}
	if records[0].SchemaVersion != traceSchemaVersion ||
		records[0].RecordID == "" ||
		records[0].SourceSchemaVersion != domain.LocalSchemaVersion ||
		records[0].AttemptIndex != 1 ||
		records[0].Source != "trajectory" ||
		records[0].Type != string(domain.TrajectoryEventAgentLLMRequest) ||
		records[0].EventID != "step-000001" ||
		records[0].OutputRef == "" ||
		records[0].RawRef == "" ||
		records[0].BodyRef != "" ||
		records[0].Metadata["runtime_type"] != string(domain.AgentRuntimeTypeAgentImage) ||
		records[0].Metadata["runtime_mount_target"] != "/opt/axern/agents/claude-code" ||
		records[0].Cost == nil ||
		len(records[0].Artifacts) != 1 ||
		records[0].Refs.RawLogRef == "" {
		t.Fatalf("trajectory record = %#v", records[0])
	}
	if _, ok := records[0].Metadata["debug_secret"]; ok {
		t.Fatalf("trajectory metadata exported unsafe key: %#v", records[0].Metadata)
	}
	if records[1].Source != "agent.raw" ||
		records[1].Type != "agent.command_started" ||
		records[1].AttemptIndex != 1 ||
		records[1].RecordID == "" ||
		records[1].EventID != "raw-000001" ||
		records[1].Line != 1 ||
		records[1].CWD != "/workspace" ||
		records[1].User != "axern" ||
		records[1].TimeoutSec != 1800 ||
		records[1].LauncherKind != domain.AgentLauncherKindAgentImage ||
		records[1].RuntimeType != domain.AgentRuntimeTypeAgentImage ||
		records[1].RuntimeImage != "axern/claude-code-bundle:dev" ||
		records[1].RuntimeMountTarget != "/opt/axern/agents/claude-code" ||
		records[1].RuntimeBinDir != "/opt/axern/agents/claude-code/bin" ||
		records[1].RuntimeProfile != "profile-a" {
		t.Fatalf("raw command record = %#v", records[1])
	}
	traceData, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read trace export: %v", err)
	}
	traceText := string(traceData)
	for _, forbidden := range []string{
		`"command"`,
		`"command_text"`,
		"--api-key",
		"sk-test-secret",
	} {
		if strings.Contains(traceText, forbidden) {
			t.Fatalf("trace export contains forbidden token %q: %s", forbidden, traceText)
		}
	}
	if records[2].Source != "agent.raw" ||
		records[2].Type != "llm.request" ||
		records[2].EventID != "raw-000002" ||
		records[2].Line != 2 ||
		records[2].BodyRef == "" ||
		records[2].RequestRef != "request-000001" {
		t.Fatalf("raw llm record = %#v", records[2])
	}
}

func TestExportPreferenceProducesPairRecords(t *testing.T) {
	runDir := createPreferenceFixture(t)
	output := filepath.Join(t.TempDir(), "preference.jsonl")

	result, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatPreference})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if result.RecordCount != 1 {
		t.Fatalf("record count = %d, want 1", result.RecordCount)
	}
	records := readExportJSONLines[PreferenceRecord](t, output)
	if len(records) != 1 {
		t.Fatalf("len(records) = %d", len(records))
	}
	rec := records[0]
	if rec.SchemaVersion != preferenceSchemaVersion ||
		rec.RecordID == "" ||
		rec.TaskID != "multi-task" ||
		rec.Instruction != "Do something" {
		t.Fatalf("record = %#v", rec)
	}
	if rec.Chosen.AgentStatus != domain.AgentStatusCompleted {
		t.Fatalf("chosen.agent_status = %q, want completed", rec.Chosen.AgentStatus)
	}
	if rec.Rejected.AgentStatus != domain.AgentStatusCompleted {
		t.Fatalf("rejected.agent_status = %q, want completed (verifier failed)", rec.Rejected.AgentStatus)
	}
	if rec.Chosen.Reward.Passed == nil || !*rec.Chosen.Reward.Passed {
		t.Fatalf("chosen.reward.passed = %v, want true", rec.Chosen.Reward.Passed)
	}
	if rec.Rejected.Reward.Passed == nil || *rec.Rejected.Reward.Passed {
		t.Fatalf("rejected.reward.passed = %v, want false", rec.Rejected.Reward.Passed)
	}
}

func TestExportPreferenceSkipsTasksWithOnlyOneSide(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "preference.jsonl")

	result, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatPreference})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if result.RecordCount != 0 {
		t.Fatalf("record count = %d, want 0 (only one outcome)", result.RecordCount)
	}
}

// TestExportPreferenceExcludesInfraFailuresFromRejectedArm verifies that
// episodes with FailureClass set (infra, timeout, etc.) are excluded from
// the rejected arm. Only verifier-rejected episodes (Passed=false, FailureClass="")
// should form pairs with chosen episodes.
func TestExportPreferenceExcludesInfraFailuresFromRejectedArm(t *testing.T) {
	runDir := createInfraFailurePreferenceFixture(t)
	output := filepath.Join(t.TempDir(), "preference.jsonl")

	result, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatPreference})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	// One chosen (passed) paired with one verifier-rejected only.
	// Infra failure must not appear as rejected.
	if result.RecordCount != 1 {
		t.Fatalf("record count = %d, want 1 (infra failure excluded)", result.RecordCount)
	}
	records := readExportJSONLines[PreferenceRecord](t, output)
	rec := records[0]
	if rec.Rejected.AgentStatus != domain.AgentStatusCompleted {
		t.Fatalf("rejected arm should be verifier-rejected (agent completed), got status=%q", rec.Rejected.AgentStatus)
	}
	if rec.Rejected.Reward.Passed == nil || *rec.Rejected.Reward.Passed {
		t.Fatalf("rejected arm reward.passed = %v, want false", rec.Rejected.Reward.Passed)
	}
}

func createPreferenceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runDir := filepath.Join(root, "pref-run")
	taskID := "multi-task"
	taskDir := filepath.Join(runDir, "tasks", taskID)
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	run := domain.RolloutRun{
		SchemaVersion: domain.LocalSchemaVersion,
		ID:            "pref-run",
		Status:        domain.RunStatusCompleted,
		CreatedAt:     now,
		Agent: domain.AgentSpec{
			Name:           "claude-code",
			Profile:        "profile-a",
			ApprovalPolicy: domain.AgentApprovalPolicyNever,
		},
		Model:           domain.ModelSpec{ID: "model-x"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		Concurrency:     1,
		AttemptsPerTask: 2,
		TaskIDs:         []string{taskID},
		OutputPath:      runDir,
		Summary: &domain.RunSummary{
			TaskCount: 1, EpisodeCount: 2,
			CompletedEpisodes:      1,
			FailedEpisodes:         1,
			VerifierFailedEpisodes: 1,
		},
	}
	task := domain.TaskInstance{
		ID:          taskID,
		Instruction: "Do something",
		Sandbox:     run.Sandbox,
		Verifier:    domain.VerifierSpec{Type: "shell", Command: "test ok"},
		Tags:        []string{},
	}

	passedTrue := true
	passedFalse := false
	exitZero := 0
	exitOne := 1
	score1 := 1.0
	score0 := 0.0
	ep1ID := "episode_pref-run_multi-task_1"
	ep2ID := "episode_pref-run_multi-task_2"

	passedEpisode := domain.Episode{
		ID: ep1ID, RunID: run.ID, TaskID: taskID, AttemptIndex: 1,
		Status: domain.EpisodeStatusCompleted, StartedAt: &now, FinishedAt: &now, CompletedAt: &now,
		Agent: run.Agent, Model: run.Model, Sandbox: run.Sandbox,
		AgentResultPath:      "episodes/" + ep1ID + "/agent.json",
		VerifierResultPath:   "episodes/" + ep1ID + "/verifier.json",
		RewardPath:           "episodes/" + ep1ID + "/reward.json",
		TrajectoryPath:       "episodes/" + ep1ID + "/trajectory.jsonl",
		ArtifactDir:          "episodes/" + ep1ID + "/artifacts",
		ArtifactManifestPath: "episodes/" + ep1ID + "/artifacts/manifest.json",
	}
	failedEpisode := domain.Episode{
		ID: ep2ID, RunID: run.ID, TaskID: taskID, AttemptIndex: 2,
		Status: domain.EpisodeStatusFailed, StartedAt: &now, FinishedAt: &now, CompletedAt: &now,
		FailureClass: domain.FailureClassVerifierFailed,
		Agent:        run.Agent, Model: run.Model, Sandbox: run.Sandbox,
		AgentResultPath:      "episodes/" + ep2ID + "/agent.json",
		VerifierResultPath:   "episodes/" + ep2ID + "/verifier.json",
		RewardPath:           "episodes/" + ep2ID + "/reward.json",
		TrajectoryPath:       "episodes/" + ep2ID + "/trajectory.jsonl",
		ArtifactDir:          "episodes/" + ep2ID + "/artifacts",
		ArtifactManifestPath: "episodes/" + ep2ID + "/artifacts/manifest.json",
	}

	writeFixtureJSON(t, filepath.Join(runDir, "run.json"), run)
	writeFixtureJSON(t, filepath.Join(runDir, "plan.json"), domain.RolloutPlan{
		SchemaVersion: domain.LocalSchemaVersion,
		RunID:         run.ID,
		CreatedAt:     run.CreatedAt,
		Selection: domain.TaskSelection{
			ResolvedTaskCount: 1,
			SelectedTaskCount: 1,
		},
		Concurrency:     1,
		AttemptsPerTask: 2,
		Agent:           run.Agent,
		Provider:        &domain.ProviderRequirement{WireAPI: "anthropic_messages"},
		Model:           run.Model,
		Sandbox:         run.Sandbox,
		TaskIDs:         []string{taskID},
		Episodes: []domain.PlannedEpisode{
			{ID: ep1ID, TaskID: taskID, AttemptIndex: 1, Order: 1},
			{ID: ep2ID, TaskID: taskID, AttemptIndex: 2, Order: 2},
		},
	})
	writeFixtureJSON(t, filepath.Join(taskDir, "task.json"), task)
	for _, ep := range []domain.Episode{passedEpisode, failedEpisode} {
		passed := ep.AttemptIndex == 1
		var exitCode int
		var score float64
		if passed {
			exitCode = exitZero
			score = score1
		} else {
			exitCode = exitOne
			score = score0
		}
		epDir := filepath.Join(runDir, "episodes", ep.ID)
		writeFixtureJSON(t, filepath.Join(epDir, "episode.json"), ep)
		writeFixtureJSON(t, filepath.Join(epDir, "agent.json"), domain.AgentResult{
			Status: domain.AgentStatusCompleted,
			Stdout: "output\n",
		})
		verifierStatus := domain.EpisodeStatusCompleted
		if !passed {
			verifierStatus = domain.EpisodeStatusFailed
		}
		writeFixtureJSON(t, filepath.Join(epDir, "verifier.json"), domain.VerifierResult{
			Status:   verifierStatus,
			Type:     "shell",
			ExitCode: &exitCode,
		})
		passedVal := passed
		if passed {
			_ = passedTrue
			passedVal = true
		} else {
			_ = passedFalse
			passedVal = false
		}
		writeFixtureJSON(t, filepath.Join(epDir, "reward.json"), domain.Reward{
			Status: domain.RewardStatusScored, Score: &score, Passed: &passedVal, Final: true,
		})
		if err := os.WriteFile(filepath.Join(epDir, "trajectory.jsonl"), nil, 0o644); err != nil {
			t.Fatalf("write trajectory: %v", err)
		}
		if err := os.Mkdir(filepath.Join(epDir, "artifacts"), 0o755); err != nil {
			t.Fatalf("mkdir artifacts: %v", err)
		}
		writeEmptyArtifactManifest(t, filepath.Join(epDir, "artifacts", "manifest.json"), ep.ID, now)
	}
	_ = exitZero
	_ = exitOne
	return runDir
}

func TestExportRejectsExistingOutput(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "sft.jsonl")
	if err := os.WriteFile(output, []byte("exists"), 0o644); err != nil {
		t.Fatalf("write output: %v", err)
	}
	if _, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatSFT}); err == nil {
		t.Fatal("Export error = nil")
	}
}

func TestExportDoesNotCreateOutputWhenRunRecordsAreInvalid(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "sft.jsonl")
	if err := os.Remove(filepath.Join(runDir, "episodes", "episode_test-run_smoke-task_1", "reward.json")); err != nil {
		t.Fatalf("remove reward: %v", err)
	}
	if _, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatSFT}); err == nil {
		t.Fatal("Export error = nil")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("output exists or stat failed: %v", err)
	}
}

func TestExportRunsSchemaValidationBeforeWritingOutput(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "sft.jsonl")
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_smoke-task_1", "agent.json")
	var agent domain.AgentResult
	readTestJSON(t, agentPath, &agent)
	agent.Artifacts = append(agent.Artifacts, domain.ArtifactRef{Path: "/tmp/outside", Kind: domain.ArtifactKindAgentRawLog})
	writeFixtureJSON(t, agentPath, agent)

	if _, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatSFT}); err == nil {
		t.Fatal("Export error = nil")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("output exists or stat failed: %v", err)
	}
}

func TestExportSkipsEpisodeWhenLLMTelemetryMissingRawLogRef(t *testing.T) {
	runDir := createExportFixture(t)
	output := filepath.Join(t.TempDir(), "sft.jsonl")
	agentPath := filepath.Join(runDir, "episodes", "episode_test-run_smoke-task_1", "agent.json")
	var agent domain.AgentResult
	readTestJSON(t, agentPath, &agent)
	agent.RawLogRef = ""
	agent.LLMRequestCount = 1
	writeFixtureJSON(t, agentPath, agent)

	result, err := Export(Params{RunDir: runDir, OutputPath: output, Format: FormatSFT})
	if err != nil {
		t.Fatalf("Export returned error: %v", err)
	}
	if result.RecordCount != 0 {
		t.Fatalf("result = %#v", result)
	}
	records := readExportJSONLines[SFTRecord](t, output)
	if len(records) != 0 {
		t.Fatalf("records = %#v", records)
	}
}

func createExportFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runDir := filepath.Join(root, "test-run")
	episodeID := "episode_test-run_smoke-task_1"
	taskDir := filepath.Join(runDir, "tasks", "smoke-task")
	episodeDir := filepath.Join(runDir, "episodes", episodeID)
	artifactDir := filepath.Join(episodeDir, "artifacts")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(artifactDir, "llm"), 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	run := domain.RolloutRun{
		SchemaVersion: domain.LocalSchemaVersion,
		ID:            "test-run",
		Status:        domain.RunStatusCompleted,
		CreatedAt:     now,
		Agent: domain.AgentSpec{
			Name:           "claude-code",
			Profile:        "profile-a",
			ApprovalPolicy: domain.AgentApprovalPolicyNever,
			Runtime: &domain.AgentRuntimeSpec{
				Type:           domain.AgentRuntimeTypeAgentImage,
				Image:          "axern/claude-code-bundle:dev",
				MountTarget:    "/opt/axern/agents/claude-code",
				BinDir:         "/opt/axern/agents/claude-code/bin",
				Profile:        "profile-a",
				Env:            map[string]string{"PROVIDER_API_KEY": "sk-test-secret"},
				Workdir:        "/workspace",
				TimeoutSec:     1800,
				AllowedTools:   []string{"Bash", "Edit"},
				IdleTimeoutSec: 30,
				Prompt: &domain.PromptSpec{
					Source: domain.PromptSourceInline,
					Inline: "inline secret prompt",
					Rounds: []domain.PromptRoundSpec{{
						Index:     1,
						Source:    domain.PromptSourceInline,
						Inline:    "round secret prompt",
						SessionID: "session-secret",
					}},
				},
				Session: &domain.AgentSessionSpec{Mode: domain.AgentSessionModeResume, SessionID: "session-secret"},
				Artifacts: &domain.ArtifactPolicySpec{
					PatchPath:     "/tmp/solution.patch",
					CaptureStdout: true,
					CaptureStderr: true,
					CaptureRawLog: true,
				},
			},
		},
		Model:           domain.ModelSpec{ID: "deepseek-v4-flash"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		Concurrency:     1,
		AttemptsPerTask: 1,
		TaskIDs:         []string{"smoke-task"},
		Summary:         &domain.RunSummary{TaskCount: 1, EpisodeCount: 1, CompletedEpisodes: 1},
		OutputPath:      runDir,
	}
	task := domain.TaskInstance{
		ID:          "smoke-task",
		Instruction: "Reply with ok",
		Source:      &domain.SourceRef{Type: "inline"},
		Sandbox:     run.Sandbox,
		Verifier:    domain.VerifierSpec{Type: "shell", Command: "test ok"},
		Tags:        []string{"smoke"},
	}
	episode := domain.Episode{
		ID:                   episodeID,
		RunID:                run.ID,
		TaskID:               task.ID,
		AttemptIndex:         1,
		Status:               domain.EpisodeStatusCompleted,
		StartedAt:            &now,
		FinishedAt:           &now,
		CompletedAt:          &now,
		Agent:                run.Agent,
		Model:                run.Model,
		Sandbox:              run.Sandbox,
		TrajectoryPath:       "episodes/" + episodeID + "/trajectory.jsonl",
		AgentResultPath:      "episodes/" + episodeID + "/agent.json",
		VerifierResultPath:   "episodes/" + episodeID + "/verifier.json",
		RewardPath:           "episodes/" + episodeID + "/reward.json",
		ArtifactDir:          "episodes/" + episodeID + "/artifacts",
		ArtifactManifestPath: "episodes/" + episodeID + "/artifacts/manifest.json",
		Usage:                &domain.UsageMetrics{InputTokens: 3, OutputTokens: 1, TotalTokens: 4},
		Cost:                 &domain.CostMetrics{Amount: 0.01, Currency: "USD"},
	}
	exitCode := 0
	score := 1.0
	passed := true
	agent := domain.AgentResult{
		Status:    domain.AgentStatusCompleted,
		Summary:   "done",
		Stdout:    "ok\n",
		RawLogRef: "episodes/" + episodeID + "/artifacts/agent.raw.jsonl",
		Artifacts: []domain.ArtifactRef{
			{Path: "episodes/" + episodeID + "/artifacts/agent.raw.jsonl", Kind: "agent_raw_log"},
			{Path: "episodes/" + episodeID + "/artifacts/llm", Kind: "llm_telemetry"},
		},
	}
	verifier := domain.VerifierResult{Status: domain.EpisodeStatusCompleted, Type: "shell", ExitCode: &exitCode}
	reward := domain.Reward{Status: domain.RewardStatusScored, Score: &score, Passed: &passed, Final: true}

	writeFixtureJSON(t, filepath.Join(runDir, "run.json"), run)
	writeFixtureJSON(t, filepath.Join(runDir, "plan.json"), domain.RolloutPlan{
		SchemaVersion: domain.LocalSchemaVersion,
		RunID:         run.ID,
		CreatedAt:     run.CreatedAt,
		Selection: domain.TaskSelection{
			ResolvedTaskCount: 1,
			SelectedTaskCount: 1,
		},
		Concurrency:     run.Concurrency,
		AttemptsPerTask: run.AttemptsPerTask,
		Agent:           run.Agent,
		Provider:        &domain.ProviderRequirement{WireAPI: "anthropic_messages"},
		Model:           run.Model,
		Sandbox:         run.Sandbox,
		TaskIDs:         []string{task.ID},
		Episodes: []domain.PlannedEpisode{{
			ID:           episode.ID,
			TaskID:       task.ID,
			AttemptIndex: episode.AttemptIndex,
			Order:        1,
		}},
	})
	writeFixtureJSON(t, filepath.Join(taskDir, "task.json"), task)
	writeFixtureJSON(t, filepath.Join(episodeDir, "episode.json"), episode)
	writeFixtureJSON(t, filepath.Join(episodeDir, "agent.json"), agent)
	writeFixtureJSON(t, filepath.Join(episodeDir, "verifier.json"), verifier)
	writeFixtureJSON(t, filepath.Join(episodeDir, "reward.json"), reward)
	trajectoryStep := domain.TrajectoryStep{
		EventID:   "step-000001",
		Index:     1,
		Timestamp: now,
		Type:      domain.TrajectoryEventAgentLLMRequest,
		Actor:     "claude-code",
		Summary:   "captured request",
		OutputRef: agent.RawLogRef,
		RawRef:    agent.RawLogRef,
		Cost:      episode.Cost,
		Metadata: domain.KeyValue{
			"runtime_type":         string(domain.AgentRuntimeTypeAgentImage),
			"runtime_image":        "axern/claude-code-bundle:dev",
			"runtime_mount_target": "/opt/axern/agents/claude-code",
			"runtime_bin_dir":      "/opt/axern/agents/claude-code/bin",
			"runtime_profile":      "profile-a",
			"debug_secret":         "sk-test-secret",
		},
		Artifacts: []domain.ArtifactRef{
			{Path: agent.RawLogRef, Kind: "agent_raw_log", Role: "raw"},
		},
	}
	trajectoryPayload, err := json.Marshal(trajectoryStep)
	if err != nil {
		t.Fatalf("marshal trajectory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(episodeDir, "trajectory.jsonl"), append(trajectoryPayload, '\n'), 0o644); err != nil {
		t.Fatalf("write trajectory: %v", err)
	}
	rawPayload := []byte(
		`{"event_id":"raw-000001","type":"agent.command_started","timestamp":"2026-05-19T12:00:00Z","launcher_kind":"agent-image","runtime_type":"agent-image","runtime_image":"axern/claude-code-bundle:dev","runtime_mount_target":"/opt/axern/agents/claude-code","runtime_bin_dir":"/opt/axern/agents/claude-code/bin","runtime_profile":"profile-a","command_text":"claude -p ok --api-key=sk-test-secret","cwd":"/workspace","user":"axern","timeout_sec":1800}` + "\n" +
			`{"event_id":"raw-000002","type":"llm.request","timestamp":"2026-05-19T12:00:00Z","method":"POST","path":"/v1/messages","model":"model","body_ref":"episodes/episode_test-run_smoke-task_1/artifacts/llm/request-000001.body","request_ref":"request-000001"}` + "\n",
	)
	if err := os.MkdirAll(filepath.Join(artifactDir, "llm"), 0o755); err != nil {
		t.Fatalf("mkdir llm artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "llm", "request-000001.body"), []byte(`{"model":"model"}`), 0o644); err != nil {
		t.Fatalf("write request body: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "agent.raw.jsonl"), rawPayload, 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	writeEmptyArtifactManifest(t, filepath.Join(artifactDir, "manifest.json"), episodeID, now)
	return runDir
}

func writeEmptyArtifactManifest(t *testing.T, path string, episodeID string, generatedAt time.Time) {
	t.Helper()
	writeFixtureJSON(t, path, domain.ArtifactManifest{
		SchemaVersion: domain.LocalSchemaVersion,
		EpisodeID:     episodeID,
		GeneratedAt:   generatedAt,
		Entries:       []domain.ArtifactManifestEntry{},
	})
}

func writeFixtureJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// createInfraFailurePreferenceFixture builds a run with three episodes for a
// single task: one verifier-passed (chosen), one verifier-failed (rejected),
// and one infrastructure-failed (must be excluded from preference pairs).
func createInfraFailurePreferenceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runDir := filepath.Join(root, "infra-pref-run")
	taskID := "infra-task"
	taskDir := filepath.Join(runDir, "tasks", taskID)
	now := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)

	run := domain.RolloutRun{
		SchemaVersion: domain.LocalSchemaVersion,
		ID:            "infra-pref-run",
		Status:        domain.RunStatusFailed,
		CreatedAt:     now,
		Agent: domain.AgentSpec{
			Name:           "claude-code",
			Profile:        "profile-a",
			ApprovalPolicy: domain.AgentApprovalPolicyNever,
		},
		Model:           domain.ModelSpec{ID: "model-x"},
		Sandbox:         domain.SandboxSpec{Backend: "axern"},
		Concurrency:     1,
		AttemptsPerTask: 3,
		TaskIDs:         []string{taskID},
		OutputPath:      runDir,
		Summary: &domain.RunSummary{
			TaskCount: 1, EpisodeCount: 3,
			CompletedEpisodes:      1,
			FailedEpisodes:         2,
			VerifierFailedEpisodes: 1,
			InfraFailures:          1,
		},
	}
	task := domain.TaskInstance{
		ID:          taskID,
		Instruction: "Infra task",
		Sandbox:     run.Sandbox,
		Verifier:    domain.VerifierSpec{Type: "shell", Command: "true"},
		Tags:        []string{},
	}

	ep1ID := "episode_infra-pref-run_infra-task_1"
	ep2ID := "episode_infra-pref-run_infra-task_2"
	ep3ID := "episode_infra-pref-run_infra-task_3"

	passedTrue := true
	passedFalse := false
	exitZero := 0
	exitOne := 1
	score1 := 1.0
	score0 := 0.0

	type epFixture struct {
		id           string
		attemptIndex int
		status       domain.EpisodeStatus
		failureClass domain.FailureClass
		agentStatus  domain.AgentStatus
		rewardPassed *bool
		rewardScore  *float64
		rewardStatus domain.RewardStatus
		exitCode     *int
	}

	fixtures := []epFixture{
		{
			id: ep1ID, attemptIndex: 1,
			status:       domain.EpisodeStatusCompleted,
			failureClass: "",
			agentStatus:  domain.AgentStatusCompleted,
			rewardPassed: &passedTrue, rewardScore: &score1,
			rewardStatus: domain.RewardStatusScored,
			exitCode:     &exitZero,
		},
		{
			id: ep2ID, attemptIndex: 2,
			status:       domain.EpisodeStatusFailed,
			failureClass: domain.FailureClassVerifierFailed,
			agentStatus:  domain.AgentStatusCompleted,
			rewardPassed: &passedFalse, rewardScore: &score0,
			rewardStatus: domain.RewardStatusScored,
			exitCode:     &exitOne,
		},
		{
			id: ep3ID, attemptIndex: 3,
			status:       domain.EpisodeStatusFailed,
			failureClass: domain.FailureClassInfrastructure,
			agentStatus:  domain.AgentStatusFailed,
			rewardPassed: nil, rewardScore: nil,
			rewardStatus: domain.RewardStatusInfraFailed,
			exitCode:     nil,
		},
	}

	episodes := make([]domain.PlannedEpisode, 0, len(fixtures))
	for i, f := range fixtures {
		episodes = append(episodes, domain.PlannedEpisode{
			ID: f.id, TaskID: taskID, AttemptIndex: f.attemptIndex, Order: i + 1,
		})
		ep := domain.Episode{
			ID: f.id, RunID: run.ID, TaskID: taskID, AttemptIndex: f.attemptIndex,
			Status: f.status, StartedAt: &now, FinishedAt: &now, CompletedAt: &now,
			FailureClass: f.failureClass,
			Agent:        run.Agent, Model: run.Model, Sandbox: run.Sandbox,
			AgentResultPath:      "episodes/" + f.id + "/agent.json",
			VerifierResultPath:   "episodes/" + f.id + "/verifier.json",
			RewardPath:           "episodes/" + f.id + "/reward.json",
			TrajectoryPath:       "episodes/" + f.id + "/trajectory.jsonl",
			ArtifactDir:          "episodes/" + f.id + "/artifacts",
			ArtifactManifestPath: "episodes/" + f.id + "/artifacts/manifest.json",
		}
		epDir := filepath.Join(runDir, "episodes", f.id)
		writeFixtureJSON(t, filepath.Join(epDir, "episode.json"), ep)
		writeFixtureJSON(t, filepath.Join(epDir, "agent.json"), domain.AgentResult{
			Status: f.agentStatus,
			Stdout: "output\n",
		})
		verifierStatus := domain.EpisodeStatusCompleted
		if f.failureClass == domain.FailureClassVerifierFailed {
			verifierStatus = domain.EpisodeStatusFailed
		}
		writeFixtureJSON(t, filepath.Join(epDir, "verifier.json"), domain.VerifierResult{
			Status:   verifierStatus,
			Type:     "shell",
			ExitCode: f.exitCode,
		})
		writeFixtureJSON(t, filepath.Join(epDir, "reward.json"), domain.Reward{
			Status: f.rewardStatus, Score: f.rewardScore, Passed: f.rewardPassed, Final: true,
		})
		if err := os.WriteFile(filepath.Join(epDir, "trajectory.jsonl"), nil, 0o644); err != nil {
			t.Fatalf("write trajectory: %v", err)
		}
		if err := os.Mkdir(filepath.Join(epDir, "artifacts"), 0o755); err != nil {
			t.Fatalf("mkdir artifacts: %v", err)
		}
		writeEmptyArtifactManifest(t, filepath.Join(epDir, "artifacts", "manifest.json"), f.id, now)
	}

	writeFixtureJSON(t, filepath.Join(runDir, "run.json"), run)
	writeFixtureJSON(t, filepath.Join(runDir, "plan.json"), domain.RolloutPlan{
		SchemaVersion: domain.LocalSchemaVersion,
		RunID:         run.ID,
		CreatedAt:     run.CreatedAt,
		Selection: domain.TaskSelection{
			ResolvedTaskCount: 1,
			SelectedTaskCount: 1,
		},
		Concurrency:     1,
		AttemptsPerTask: 3,
		Agent:           run.Agent,
		Provider:        &domain.ProviderRequirement{WireAPI: "anthropic_messages"},
		Model:           run.Model,
		Sandbox:         run.Sandbox,
		TaskIDs:         []string{taskID},
		Episodes:        episodes,
	})
	writeFixtureJSON(t, filepath.Join(taskDir, "task.json"), task)
	return runDir
}

func readExportJSONLines[T any](t *testing.T, path string) []T {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	var records []T
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var record T
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode line: %v", err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return records
}
