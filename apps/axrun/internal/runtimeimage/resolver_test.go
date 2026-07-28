package runtimeimage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestDockerResolverBuildsAndPushesDeterministicImage(t *testing.T) {
	runDir := t.TempDir()
	dockerfile := filepath.Join(runDir, "inputs", "task", "Dockerfile")
	if err := os.MkdirAll(filepath.Dir(dockerfile), 0o755); err != nil {
		t.Fatalf("mkdir Dockerfile dir: %v", err)
	}
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dockerfile), "seed.txt"), []byte("seed"), 0o644); err != nil {
		t.Fatalf("write context seed: %v", err)
	}
	runner := &recordingCommandRunner{}
	resolver := DockerResolver{Repository: "localhost:5000/axrun/tasks", Runner: runner}
	request := Request{
		RunDir: runDir,
		Task: domain.TaskInstance{
			ID: "Smoke_Task",
			Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
				Type:       domain.SandboxRuntimeSourceDockerfile,
				Dockerfile: "inputs/task/Dockerfile",
			}},
		},
		Episode: domain.Episode{RunID: "test-run"},
	}

	result, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if result.Repository != "localhost:5000/axrun/tasks" ||
		result.PushRepository != "localhost:5000/axrun/tasks" ||
		result.Delivery != DeliveryRegistry ||
		!strings.HasPrefix(result.Tag, "axrun-smoke_task-") ||
		result.Image != result.Repository+":"+result.Tag ||
		result.PushImage != result.PushRepository+":"+result.Tag ||
		result.ContextRef != "inputs/task" ||
		result.ContextDir != filepath.Dir(dockerfile) ||
		result.DockerfilePath != dockerfile ||
		result.DockerfileRef != "inputs/task/Dockerfile" {
		t.Fatalf("result = %#v", result)
	}
	wantCommands := [][]string{
		{"docker", "build", "-f", dockerfile, "-t", result.PushImage, filepath.Dir(dockerfile)},
		{"docker", "push", result.PushImage},
	}
	if !reflect.DeepEqual(runner.commands, wantCommands) {
		t.Fatalf("commands = %#v want %#v", runner.commands, wantCommands)
	}

	secondTag, err := ImageTag(request, filepath.Dir(dockerfile), "inputs/task/Dockerfile")
	if err != nil {
		t.Fatalf("ImageTag returned error: %v", err)
	}
	if secondTag != result.Tag {
		t.Fatalf("second tag = %q want %q", secondTag, result.Tag)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dockerfile), "seed.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("rewrite context seed: %v", err)
	}
	changedTag, err := ImageTag(request, filepath.Dir(dockerfile), "inputs/task/Dockerfile")
	if err != nil {
		t.Fatalf("ImageTag after context change returned error: %v", err)
	}
	if changedTag == result.Tag {
		t.Fatalf("changed tag = %q, want different tag after context change", changedTag)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(data), runDir) ||
		!strings.Contains(string(data), `"dockerfile_ref":"inputs/task/Dockerfile"`) ||
		!strings.Contains(string(data), `"context_ref":"inputs/task"`) {
		t.Fatalf("result json = %s", data)
	}
}

func TestDockerResolverUsesSeparatePushRepository(t *testing.T) {
	runDir := t.TempDir()
	dockerfile := filepath.Join(runDir, "inputs", "task", "Dockerfile")
	if err := os.MkdirAll(filepath.Dir(dockerfile), 0o755); err != nil {
		t.Fatalf("mkdir Dockerfile dir: %v", err)
	}
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	runner := &recordingCommandRunner{}
	resolver := DockerResolver{
		Repository:     "host.docker.internal:5001/axrun/tasks",
		PushRepository: "localhost:5001/axrun/tasks",
		Runner:         runner,
	}
	result, err := resolver.Resolve(context.Background(), Request{
		RunDir: runDir,
		Task: domain.TaskInstance{
			ID: "smoke-task",
			Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
				Type:       domain.SandboxRuntimeSourceDockerfile,
				Dockerfile: "inputs/task/Dockerfile",
			}},
		},
		Episode: domain.Episode{RunID: "test-run"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if !strings.HasPrefix(result.Image, "host.docker.internal:5001/axrun/tasks:") ||
		!strings.HasPrefix(result.PushImage, "localhost:5001/axrun/tasks:") {
		t.Fatalf("result = %#v", result)
	}
	wantCommands := [][]string{
		{"docker", "build", "-f", dockerfile, "-t", result.PushImage, filepath.Dir(dockerfile)},
		{"docker", "push", result.PushImage},
	}
	if !reflect.DeepEqual(runner.commands, wantCommands) {
		t.Fatalf("commands = %#v want %#v", runner.commands, wantCommands)
	}
}

func TestDockerResolverImportsIntoComposeWithoutRepository(t *testing.T) {
	runDir := t.TempDir()
	dockerfile := filepath.Join(runDir, "inputs", "task", "Dockerfile")
	if err := os.MkdirAll(filepath.Dir(dockerfile), 0o755); err != nil {
		t.Fatalf("mkdir Dockerfile dir: %v", err)
	}
	if err := os.WriteFile(dockerfile, []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	runner := &recordingCommandRunner{}
	resolver := DockerResolver{
		Delivery:       DeliveryComposeImport,
		ComposeProject: "axern-test",
		Runner:         runner,
	}
	result, err := resolver.Resolve(context.Background(), Request{
		RunDir: runDir,
		Task: domain.TaskInstance{
			ID: "smoke-task",
			Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
				Type:       domain.SandboxRuntimeSourceDockerfile,
				Dockerfile: "inputs/task/Dockerfile",
			}},
		},
		Episode: domain.Episode{RunID: "test-run"},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if result.Repository != defaultComposeRepository ||
		result.PushRepository != defaultComposeRepository ||
		result.Delivery != DeliveryComposeImport ||
		result.ImportedRef != result.Image ||
		!strings.HasPrefix(result.Image, "index.docker.io/"+result.Repository+"@sha256:") ||
		result.PushImage != result.PushRepository+":"+result.Tag {
		t.Fatalf("result = %#v", result)
	}
	if len(runner.commands) != 5 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	wantBuild := []string{"docker", "build", "-f", dockerfile, "-t", result.PushImage, filepath.Dir(dockerfile)}
	if !reflect.DeepEqual(runner.commands[0], wantBuild) {
		t.Fatalf("build command = %#v want %#v", runner.commands[0], wantBuild)
	}
	if got := runner.commands[1]; len(got) != 5 || got[0] != "docker" || got[1] != "save" || got[2] != "-o" || got[4] != result.PushImage {
		t.Fatalf("save command = %#v", got)
	}
	archivePath := runner.commands[1][3]
	if got := runner.commands[2]; len(got) != 4 || got[0] != "docker" || got[1] != "cp" || got[2] != archivePath || !strings.HasPrefix(got[3], "axern-test-node-1:/tmp/axrun-runtime-image-") {
		t.Fatalf("copy command = %#v", got)
	}
	remoteArchive := strings.TrimPrefix(runner.commands[2][3], "axern-test-node-1:")
	wantImport := []string{"docker", "exec", "axern-test-node-1", "axctl", "image", "import", "--imagemgr-socket", "/run/imagemgr/imagemgr.sock", "--archive", remoteArchive, "--ref", result.Image}
	if !reflect.DeepEqual(runner.commands[3], wantImport) {
		t.Fatalf("import command = %#v want %#v", runner.commands[3], wantImport)
	}
	wantCleanup := []string{"docker", "exec", "axern-test-node-1", "rm", "-f", remoteArchive}
	if !reflect.DeepEqual(runner.commands[4], wantCleanup) {
		t.Fatalf("cleanup command = %#v want %#v", runner.commands[4], wantCleanup)
	}
}

func TestValidateResolverConfigAllowsComposeImportWithoutRepository(t *testing.T) {
	if err := ValidateResolverConfig(DockerResolver{Delivery: DeliveryComposeImport}); err != nil {
		t.Fatalf("ValidateResolverConfig returned error: %v", err)
	}
}

func TestValidateResolverConfigRejectsUnknownDelivery(t *testing.T) {
	err := ValidateResolverConfig(DockerResolver{
		Delivery: "registry-cache",
	})
	if err == nil || !strings.Contains(err.Error(), DeliveryEnv) {
		t.Fatalf("ValidateResolverConfig error = %v", err)
	}
}

func TestDockerResolverRejectsTaggedRepository(t *testing.T) {
	resolver := DockerResolver{Repository: "example.com/axrun/tasks:latest", Runner: &recordingCommandRunner{}}
	_, err := resolver.Resolve(context.Background(), Request{
		RunDir: t.TempDir(),
		Task: domain.TaskInstance{Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
			Type:       domain.SandboxRuntimeSourceDockerfile,
			Dockerfile: "Dockerfile",
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "without tag") {
		t.Fatalf("Resolve error = %v", err)
	}
}

func TestDockerResolverRejectsTaggedPushRepository(t *testing.T) {
	resolver := DockerResolver{
		Repository:     "example.com/axrun/tasks",
		PushRepository: "localhost:5001/axrun/tasks:latest",
		Runner:         &recordingCommandRunner{},
	}
	_, err := resolver.Resolve(context.Background(), Request{
		RunDir: t.TempDir(),
		Task: domain.TaskInstance{Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
			Type:       domain.SandboxRuntimeSourceDockerfile,
			Dockerfile: "Dockerfile",
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "AXERN_AXRUN_IMAGE_PUSH_REPOSITORY") {
		t.Fatalf("Resolve error = %v", err)
	}
}

func TestDockerResolverRejectsEscapingDockerfileRef(t *testing.T) {
	resolver := DockerResolver{Repository: "example.com/axrun/tasks", Runner: &recordingCommandRunner{}}
	_, err := resolver.Resolve(context.Background(), Request{
		RunDir: t.TempDir(),
		Task: domain.TaskInstance{Sandbox: domain.SandboxSpec{RuntimeSource: &domain.SandboxRuntimeSourceSpec{
			Type:       domain.SandboxRuntimeSourceDockerfile,
			Dockerfile: "../Dockerfile",
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "run-root-relative") {
		t.Fatalf("Resolve error = %v", err)
	}
}

type recordingCommandRunner struct {
	commands [][]string
	err      error
}

func (r *recordingCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	command := append([]string{name}, args...)
	r.commands = append(r.commands, command)
	if r.err != nil {
		return []byte("command failed"), r.err
	}
	return []byte("ok"), nil
}
