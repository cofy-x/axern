package axern

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentpkg "github.com/cofy-x/axern/apps/axrun/internal/agent"
	"github.com/cofy-x/axern/apps/axrun/internal/backend"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/runtimeimage"
	"github.com/cofy-x/axern/apps/axrun/internal/sandbox"
	sandboxaxern "github.com/cofy-x/axern/apps/axrun/internal/sandbox/axern"
)

func TestRunnerExecuteAgentHarnessBeforeVerifier(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.Episode.Agent = domain.AgentSpec{Name: "claude-code"}
	agentHarness := &recordingAgent{result: agentpkg.Result{
		Status:  domain.AgentStatusCompleted,
		Summary: "agent done",
		Stdout:  "hello",
	}}
	episode, err := (Adapter{Runtime: &fakeRuntime{sandbox: &fakeSandbox{}}, Agent: agentHarness, Now: fixedNow}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted || !agentHarness.called {
		t.Fatalf("episode = %#v called=%v", episode, agentHarness.called)
	}
	var agentResult domain.AgentResult
	readJSON(t, layout.AgentJSONPath, &agentResult)
	if agentResult.Status != domain.AgentStatusCompleted || agentResult.StdoutRef == "" || len(agentResult.TrajectoryStepRefs) == 0 {
		t.Fatalf("agent result = %#v", agentResult)
	}
}

func TestRunnerPreflightSkipsClaudeCodeConfigForNonClaudeAgent(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "not-an-int")
	err := (Adapter{
		Runtime:   &fakeRuntime{},
		AgentName: "oracle",
		Registry:  testRegistry(),
	}).Preflight()
	if err != nil {
		t.Fatalf("Preflight returned error: %v", err)
	}
}

func TestRunnerPreflightChecksClaudeCodeConfigForClaudeAgent(t *testing.T) {
	t.Setenv("AXRUN_CLAUDE_CODE_TIMEOUT_SEC", "not-an-int")
	err := (Adapter{
		Runtime:   &fakeRuntime{},
		AgentName: "claude-code",
		Registry:  testRegistry(),
	}).Preflight()
	if err == nil {
		t.Fatal("Preflight error = nil, want Claude Code config error")
	}
}

func TestRunnerConfigForTaskUsesTaskImage(t *testing.T) {
	task := domain.TaskInstance{
		Sandbox: domain.SandboxSpec{
			RuntimeSource: &domain.SandboxRuntimeSourceSpec{
				Type:  domain.SandboxRuntimeSourceImage,
				Image: "example.com/task:latest",
			},
		},
	}
	config, err := (Adapter{Config: Config{Endpoint: "127.0.0.1:24000"}}).configForTask(task)
	if err != nil {
		t.Fatalf("configForTask returned error: %v", err)
	}
	if config.Image != "example.com/task:latest" {
		t.Fatalf("config = %#v", config)
	}
}

func TestRunnerConfigForTaskPrefersTaskSourceOverConfiguredDefault(t *testing.T) {
	task := domain.TaskInstance{
		Sandbox: domain.SandboxSpec{
			RuntimeSource: &domain.SandboxRuntimeSourceSpec{
				Type:  domain.SandboxRuntimeSourceImage,
				Image: "example.com/task:latest",
			},
		},
	}
	config, err := (Adapter{Config: Config{Endpoint: "127.0.0.1:24000", TemplateID: "claude-code"}}).configForTask(task)
	if err != nil {
		t.Fatalf("configForTask returned error: %v", err)
	}
	if config.TemplateID != "" || config.Image != "example.com/task:latest" {
		t.Fatalf("config = %#v", config)
	}
}

func TestNewDefersRuntimeCreationForTaskSourceResolution(t *testing.T) {
	adapter := New(Config{Endpoint: "127.0.0.1:24000"})
	if adapter.Runtime != nil {
		t.Fatalf("Runtime = %#v, want nil default runtime", adapter.Runtime)
	}
}

func TestRunnerConfigForTaskRejectsDockerfileRuntimeSource(t *testing.T) {
	task := domain.TaskInstance{
		Sandbox: domain.SandboxSpec{
			RuntimeSource: &domain.SandboxRuntimeSourceSpec{
				Type:       domain.SandboxRuntimeSourceDockerfile,
				Dockerfile: "inputs/task/Dockerfile",
			},
		},
	}
	_, err := (Adapter{Config: Config{Endpoint: "127.0.0.1:24000"}}).configForTask(task)
	if err == nil {
		t.Fatal("configForTask error = nil, want dockerfile build/import error")
	}
}

func TestRunnerResolvesDockerfileRuntimeSourceBeforeExecution(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	dockerfile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(layout.ArtifactDir))), "inputs", "task", "Dockerfile")
	if err := os.MkdirAll(filepath.Dir(dockerfile), 0o755); err != nil {
		t.Fatalf("mkdir dockerfile dir: %v", err)
	}
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write dockerfile: %v", err)
	}
	layout.TaskInstance.Sandbox.RuntimeSource = &domain.SandboxRuntimeSourceSpec{
		Type:       domain.SandboxRuntimeSourceDockerfile,
		Dockerfile: "inputs/task/Dockerfile",
	}
	resolver := &fakeImageResolver{result: runtimeimage.Result{
		Image:          "example.com/axrun/smoke:built",
		Repository:     "example.com/axrun/smoke",
		Tag:            "built",
		ContextDir:     filepath.Dir(dockerfile),
		DockerfilePath: dockerfile,
		DockerfileRef:  "inputs/task/Dockerfile",
	}}
	episode, err := (Adapter{
		Runtime: &fakeRuntime{sandbox: &fakeSandbox{}},
		Images:  resolver,
		Now:     fixedNow,
	}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Sandbox.RuntimeSource == nil ||
		episode.Sandbox.RuntimeSource.Type != domain.SandboxRuntimeSourceImage ||
		episode.Sandbox.RuntimeSource.Image != "example.com/axrun/smoke:built" ||
		episode.Sandbox.RuntimeSource.Origin != domain.SandboxRuntimeSourceOriginRuntimeImageBuild {
		t.Fatalf("episode sandbox runtime source = %#v", episode.Sandbox.RuntimeSource)
	}
	if len(episode.Artifacts) != 1 ||
		episode.Artifacts[0].Kind != domain.ArtifactKindRuntimeImageBuild ||
		episode.Artifacts[0].Path != "episodes/episode_test-run_smoke-task_1/artifacts/runtime-image-build.json" {
		t.Fatalf("episode artifacts = %#v", episode.Artifacts)
	}
	if !resolver.called {
		t.Fatal("resolver was not called")
	}
	artifactPath := filepath.Join(layout.ArtifactDir, "runtime-image-build.json")
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("stat runtime image build artifact: %v", err)
	}
	data, err := os.ReadFile(layout.TrajectoryPath)
	if err != nil {
		t.Fatalf("read trajectory: %v", err)
	}
	if !strings.Contains(string(data), string(domain.TrajectoryEventSystemImageBuildStart)) ||
		!strings.Contains(string(data), string(domain.TrajectoryEventSystemImageBuildDone)) {
		t.Fatalf("trajectory = %s", data)
	}
	if strings.Contains(string(data), "output_ref") {
		t.Fatalf("runtime image ref must not be written to path-only output_ref: %s", data)
	}
}

func TestRunnerBuildsDockerfileBeforeConfiguredDefaultSource(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.TaskInstance.Sandbox.RuntimeSource = &domain.SandboxRuntimeSourceSpec{
		Type:       domain.SandboxRuntimeSourceDockerfile,
		Dockerfile: "inputs/task/Dockerfile",
	}
	resolver := &fakeImageResolver{result: runtimeimage.Result{Image: "example.com/axrun/smoke:built"}}
	_, err := (Adapter{
		Config:  Config{TemplateID: "python311"},
		Runtime: &fakeRuntime{sandbox: &fakeSandbox{}},
		Images:  resolver,
		Now:     fixedNow,
	}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !resolver.called {
		t.Fatal("resolver was not called")
	}
}

func TestAxernAdapterAcceptsAgentImageRuntimePreflight(t *testing.T) {
	err := (Adapter{}).PreflightAgent(domain.AgentSpec{
		Name: "claude-code",
		Runtime: &domain.AgentRuntimeSpec{
			Type:  domain.AgentRuntimeTypeAgentImage,
			Image: "ghcr.io/cofy-x/claude-code:latest",
		},
	})
	if err != nil {
		t.Fatalf("PreflightAgent returned error: %v", err)
	}
}

func TestAxernAdapterRunsManagedAgentImageCommand(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	layout.Episode.Agent = domain.AgentSpec{
		Name:           "claude-code",
		ApprovalPolicy: domain.AgentApprovalPolicyNever,
		Runtime: &domain.AgentRuntimeSpec{
			Type:    domain.AgentRuntimeTypeAgentImage,
			Image:   "ghcr.io/cofy-x/claude-code-bundle:latest",
			Workdir: "/workspace",
			User:    "axern",
			Env:     map[string]string{"CUSTOM": "value"},
		},
	}
	sb := &fakeSandbox{execResult: sandbox.ExecResult{ExitCode: 0, Stdout: "image-agent-ok"}}
	episode, err := (Adapter{
		Config:   Config{Endpoint: "127.0.0.1:24000", TemplateID: "python311"},
		Runtime:  &fakeRuntime{instance: sb},
		Registry: testRegistry(),
		Now:      fixedNow,
	}).Execute(executeRequest(store, layout))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if episode.Status != domain.EpisodeStatusCompleted {
		t.Fatalf("episode = %#v", episode)
	}
	var agentCall *fakeExecCall
	for i := range sb.execCallsList {
		call := &sb.execCallsList[i]
		if strings.Contains(call.command.Shell(), "claude -p") && strings.Contains(call.command.Shell(), "--permission-mode bypassPermissions") {
			agentCall = call
			break
		}
	}
	if agentCall == nil {
		t.Fatalf("agent exec not found in calls: %#v", sb.execCallsList)
	}
	if agentCall.options.CWD != "/workspace" || agentCall.options.User != "axern" || agentCall.options.Env["CUSTOM"] != "value" {
		t.Fatalf("exec options = %#v", agentCall.options)
	}
	if _, ok := agentCall.options.Env["PATH"]; ok {
		t.Fatalf("exec options must preserve the runtime image PATH: %#v", agentCall.options.Env)
	}
	if !strings.Contains(agentCall.command.Shell(), "export PATH='/opt/axern/agents/claude-code/bin':\"${PATH:-") {
		t.Fatalf("agent command does not prepend the bundle bin directory: %q", agentCall.command.Shell())
	}
	if got := agentCall.options.Env["AXRUN_AGENT_BUNDLE_MOUNT_TARGET"]; got != "/opt/axern/agents/claude-code" {
		t.Fatalf("mount target env = %q", got)
	}
	var agentResult domain.AgentResult
	readJSON(t, layout.AgentJSONPath, &agentResult)
	if agentResult.LauncherKind != domain.AgentLauncherKindAgentImage ||
		agentResult.RuntimeType != domain.AgentRuntimeTypeAgentImage ||
		agentResult.RuntimeImage != "ghcr.io/cofy-x/claude-code-bundle:latest" {
		t.Fatalf("agent result = %#v", agentResult)
	}
}

func TestAxernAdapterRuntimeForRequestMountsAgentImageBundleIntoTaskSandbox(t *testing.T) {
	adapter := Adapter{
		Config: Config{Endpoint: "127.0.0.1:24000", TemplateID: "python311"},
	}
	request := backend.ExecuteRequest{
		Task: domain.TaskInstance{
			Sandbox: domain.SandboxSpec{
				RuntimeClass: "runc",
				RuntimeSource: &domain.SandboxRuntimeSourceSpec{
					Type:  domain.SandboxRuntimeSourceImage,
					Image: "example.com/task:latest",
				},
			},
		},
		Episode: domain.Episode{
			Agent: domain.AgentSpec{
				Name: "claude-code",
				Runtime: &domain.AgentRuntimeSpec{
					Type:  domain.AgentRuntimeTypeAgentImage,
					Image: "ghcr.io/cofy-x/claude-code-bundle:latest",
				},
			},
		},
	}
	runtime, err := adapter.runtimeForRequest(request)
	if err != nil {
		t.Fatalf("runtimeForRequest returned error: %v", err)
	}
	axernRuntime, ok := runtime.(sandboxaxern.Runtime)
	if !ok {
		t.Fatalf("runtime type = %T", runtime)
	}
	if axernRuntime.Config.Image != "example.com/task:latest" || axernRuntime.Config.TemplateID != "" {
		t.Fatalf("runtime config = %#v", axernRuntime.Config)
	}
	if axernRuntime.Config.RuntimeClass != "runc" {
		t.Fatalf("runtime class = %q, want runc", axernRuntime.Config.RuntimeClass)
	}
	if len(axernRuntime.Config.ImageMounts) != 1 {
		t.Fatalf("image mounts = %#v", axernRuntime.Config.ImageMounts)
	}
	mount := axernRuntime.Config.ImageMounts[0]
	if mount.Image != "ghcr.io/cofy-x/claude-code-bundle:latest" ||
		mount.Target != "/__claude_code" ||
		!mount.Readonly {
		t.Fatalf("image mount = %#v", mount)
	}
}

func TestAxernAdapterRuntimeForRequestAppliesTaskResources(t *testing.T) {
	adapter := Adapter{Config: Config{
		Endpoint:      "127.0.0.1:24000",
		TemplateID:    "default",
		RequestCPU:    "100m",
		RequestMemory: "128Mi",
	}}
	request := backend.ExecuteRequest{Task: domain.TaskInstance{
		Resources: &domain.ResourceSpec{RequestCPU: "750m", RequestMemory: "2Gi", LimitCPU: "1", LimitMemory: "4Gi"},
		Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
			Type: domain.SandboxRuntimeSourceTemplate, TemplateID: "task-template",
		}},
	}}
	runtime, err := adapter.runtimeForRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	config := runtime.(sandboxaxern.Runtime).Config
	if config.RequestCPU != "750m" || config.RequestMemory != "2Gi" || config.LimitCPU != "1" || config.LimitMemory != "4Gi" {
		t.Fatalf("task resources were not applied: %#v", config)
	}
}

func TestAxernAdapterRuntimeForRequestUsesAgentRuntimeMountTarget(t *testing.T) {
	adapter := Adapter{Config: Config{Endpoint: "127.0.0.1:24000"}}
	request := backend.ExecuteRequest{
		Task: domain.TaskInstance{
			Sandbox: domain.SandboxSpec{
				RuntimeSource: &domain.SandboxRuntimeSourceSpec{
					Type:  domain.SandboxRuntimeSourceImage,
					Image: "example.com/task:latest",
				},
			},
		},
		Episode: domain.Episode{
			Agent: domain.AgentSpec{
				Name: "custom-agent",
				Runtime: &domain.AgentRuntimeSpec{
					Type:        domain.AgentRuntimeTypeAgentImage,
					Image:       "ghcr.io/cofy-x/custom-agent-bundle:latest",
					MountTarget: "/opt/axern/agents/custom-agent",
				},
			},
		},
	}
	runtime, err := adapter.runtimeForRequest(request)
	if err != nil {
		t.Fatalf("runtimeForRequest returned error: %v", err)
	}
	axernRuntime, ok := runtime.(sandboxaxern.Runtime)
	if !ok {
		t.Fatalf("runtime type = %T", runtime)
	}
	if len(axernRuntime.Config.ImageMounts) != 1 {
		t.Fatalf("image mounts = %#v", axernRuntime.Config.ImageMounts)
	}
	if got := axernRuntime.Config.ImageMounts[0].Target; got != "/opt/axern/agents/custom-agent" {
		t.Fatalf("mount target = %q", got)
	}
}

func TestAxernAdapterRuntimeForRequestRequiresTaskOrConfiguredSandboxSource(t *testing.T) {
	adapter := Adapter{Config: Config{Endpoint: "127.0.0.1:24000"}}
	request := backend.ExecuteRequest{
		Episode: domain.Episode{
			Agent: domain.AgentSpec{
				Name: "claude-code",
				Runtime: &domain.AgentRuntimeSpec{
					Type:  domain.AgentRuntimeTypeAgentImage,
					Image: "ghcr.io/cofy-x/claude-code:latest",
				},
			},
		},
	}
	if _, err := adapter.runtimeForRequest(request); err == nil || !strings.Contains(err.Error(), "runtime_source") {
		t.Fatalf("runtimeForRequest error = %v, want missing runtime_source", err)
	}
}

func TestAxernAdapterRuntimeForRequestFallsBackToTaskRuntime(t *testing.T) {
	adapter := Adapter{
		Config: Config{Endpoint: "127.0.0.1:24000", TemplateID: "python311"},
	}
	request := backend.ExecuteRequest{
		Task: domain.TaskInstance{
			Sandbox: domain.SandboxSpec{
				RuntimeSource: &domain.SandboxRuntimeSourceSpec{
					Type:  domain.SandboxRuntimeSourceImage,
					Image: "task-image:latest",
				},
			},
		},
		Episode: domain.Episode{
			Agent: domain.AgentSpec{Name: "claude-code"},
		},
	}
	runtime, err := adapter.runtimeForRequest(request)
	if err != nil {
		t.Fatalf("runtimeForRequest returned error: %v", err)
	}
	if runtime == nil {
		t.Fatal("runtimeForRequest returned nil")
	}
}

func TestPreflightTasksRejectsNoRuntimeSourceWithoutConfiguredDefault(t *testing.T) {
	adapter := Adapter{Config: Config{Endpoint: "127.0.0.1:24000"}}
	tasks := []domain.TaskInstance{{ID: "t1", Sandbox: domain.SandboxSpec{}}}
	if err := adapter.PreflightTasks(tasks); err == nil || !strings.Contains(err.Error(), "runtime_source") {
		t.Fatalf("PreflightTasks error = %v, want missing runtime_source", err)
	}
}

func TestPreflightTasksRejectsNoRuntimeSourceWithInjectedImageResolver(t *testing.T) {
	adapter := Adapter{
		Config: Config{Endpoint: "127.0.0.1:24000"},
		Images: &fakeImageResolver{},
	}
	tasks := []domain.TaskInstance{{ID: "t1", Sandbox: domain.SandboxSpec{}}}
	if err := adapter.PreflightTasks(tasks); err == nil || !strings.Contains(err.Error(), "runtime_source") {
		t.Fatalf("PreflightTasks error = %v, want missing runtime_source", err)
	}
}

func TestPreflightTasksRejectsNoRuntimeSourceWithoutConfiguredTaskSource(t *testing.T) {
	adapter := Adapter{Config: Config{Endpoint: "127.0.0.1:24000"}}
	tasks := []domain.TaskInstance{{ID: "t1", Sandbox: domain.SandboxSpec{}}}
	if err := adapter.PreflightTasks(tasks); err == nil {
		t.Fatal("PreflightTasks error = nil, want missing source error")
	}
}

func TestPreflightTasksAcceptsNoRuntimeSourceWhenConfigHasTemplateID(t *testing.T) {
	adapter := Adapter{Config: Config{Endpoint: "127.0.0.1:24000", TemplateID: "python311"}}
	tasks := []domain.TaskInstance{{ID: "t1", Sandbox: domain.SandboxSpec{}}}
	if err := adapter.PreflightTasks(tasks); err != nil {
		t.Fatalf("PreflightTasks returned error: %v", err)
	}
}

type recordingAgent struct {
	called bool
	result agentpkg.Result
}

type fakeImageResolver struct {
	called bool
	result runtimeimage.Result
	err    error
}

func (f *fakeImageResolver) Resolve(context.Context, runtimeimage.Request) (runtimeimage.Result, error) {
	f.called = true
	return f.result, f.err
}

func (r *recordingAgent) Preflight() error {
	return nil
}

func (r *recordingAgent) Run(context.Context, agentpkg.Request) (agentpkg.Result, error) {
	r.called = true
	return r.result, nil
}
