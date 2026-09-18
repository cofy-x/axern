package localruntime

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type imageLoadRunner struct {
	outputs         map[string][]byte
	runCalls        int
	sourceArgs      []string
	destinationArgs []string
}

func (r *imageLoadRunner) Run(context.Context, io.Writer, io.Writer, string, ...string) error {
	r.runCalls++
	return nil
}

func (r *imageLoadRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	value, ok := r.outputs[key]
	if !ok {
		return nil, fmt.Errorf("unexpected output command %s", key)
	}
	return value, nil
}

func (r *imageLoadRunner) Pipe(_ context.Context, stdout, _ io.Writer, _ string, sourceArgs []string, _ string, destinationArgs []string) error {
	r.sourceArgs = append([]string(nil), sourceArgs...)
	r.destinationArgs = append([]string(nil), destinationArgs...)
	_, err := io.WriteString(stdout, `{"source_ref":"demo:dev","canonical_ref":"index.docker.io/library/demo:dev","immutable_ref":"index.docker.io/library/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","content_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","archive_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","platform":"linux/amd64","size_bytes":42,"reused":false}`)
	return err
}

func prepareManagedLocalState(t *testing.T, dir string) {
	t.Helper()
	if err := saveMetadata(filepath.Join(dir, "metadata.json"), Metadata{Version: "test", ComposeProject: ProjectName, CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	for _, name := range []string{"compose.env", "compose.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func TestComposeArgsUseStateOwnedProject(t *testing.T) {
	dir := t.TempDir()
	if err := saveMetadata(filepath.Join(dir, "metadata.json"), Metadata{Version: "test", ComposeProject: "axern-source", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	manager := &Manager{Dir: dir}
	want := []string{"compose", "--project-name", "axern-source", "--env-file", filepath.Join(dir, "compose.env"), "-f", filepath.Join(dir, "compose.yaml"), "ps"}
	if got := manager.composeArgs("", "ps"); !reflect.DeepEqual(got, want) {
		t.Fatalf("composeArgs() = %v, want %v", got, want)
	}
}

func TestImageLoadStreamsImmutableImageIDIntoNode(t *testing.T) {
	dir := t.TempDir()
	prepareManagedLocalState(t, dir)
	runner := &imageLoadRunner{outputs: map[string][]byte{
		"docker image inspect demo:dev": []byte(`[{"Id":"sha256:source","Os":"linux","Architecture":"amd64"}]`),
		"docker compose --project-name axern-local --env-file " + dir + "/compose.env -f " + dir + "/compose.yaml images -q node": []byte("sha256:node\n"),
		"docker image inspect sha256:node": []byte(`[{"Id":"sha256:node","Os":"linux","Architecture":"amd64"}]`),
	}}
	manager := &Manager{Dir: dir, Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	result, err := manager.ImageLoad(t.Context(), "demo:dev", ImageLoadOptions{})
	if err != nil {
		t.Fatalf("ImageLoad() error = %v", err)
	}
	if result.ContentDigest == "" {
		t.Fatal("ImageLoad() generation digest is empty")
	}
	resolved, pinned, err := ResolveLocalImageReference(dir, "demo:dev")
	if err != nil {
		t.Fatalf("ResolveLocalImageReference() error = %v", err)
	}
	if !pinned || resolved != result.ImmutableRef {
		t.Fatalf("ResolveLocalImageReference() = (%q, %t), want (%q, true)", resolved, pinned, result.ImmutableRef)
	}
	if !reflect.DeepEqual(runner.sourceArgs, []string{"image", "save", "sha256:source"}) {
		t.Fatalf("source args = %v", runner.sourceArgs)
	}
	wantTail := []string{"exec", "-T", "node", "axctl", "image", "import", "--file", "-", "--ref", "demo:dev", "--json"}
	if got := runner.destinationArgs[len(runner.destinationArgs)-len(wantTail):]; !reflect.DeepEqual(got, wantTail) {
		t.Fatalf("destination tail = %v, want %v", got, wantTail)
	}
}

func TestImageLoadRejectsPlatformMismatchBeforeStreaming(t *testing.T) {
	dir := t.TempDir()
	prepareManagedLocalState(t, dir)
	runner := &imageLoadRunner{outputs: map[string][]byte{
		"docker image inspect demo:dev": []byte(`[{"Id":"sha256:source","Os":"linux","Architecture":"arm64"}]`),
		"docker compose --project-name axern-local --env-file " + dir + "/compose.env -f " + dir + "/compose.yaml images -q node": []byte("sha256:node\n"),
		"docker image inspect sha256:node": []byte(`[{"Id":"sha256:node","Os":"linux","Architecture":"amd64"}]`),
	}}
	manager := &Manager{Dir: dir, Runner: runner, Stdout: io.Discard, Stderr: io.Discard}
	_, err := manager.ImageLoad(t.Context(), "demo:dev", ImageLoadOptions{})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("ImageLoad() error = %v", err)
	}
	if runner.sourceArgs != nil {
		t.Fatal("platform mismatch started streaming")
	}
}

func TestImageLoadRejectsUninitializedLocalStateBeforePull(t *testing.T) {
	dir := t.TempDir()
	runner := &imageLoadRunner{}
	manager := &Manager{Dir: dir, Runner: runner, Stdout: io.Discard, Stderr: io.Discard}

	_, err := manager.ImageLoad(t.Context(), "demo:dev", ImageLoadOptions{Pull: true})
	if err == nil || !strings.Contains(err.Error(), "only manages the CLI-owned local stack") {
		t.Fatalf("ImageLoad() error = %v", err)
	}
	if runner.runCalls != 0 {
		t.Fatalf("ImageLoad() pulled before validating local ownership")
	}
}

func TestImageLoadReportsRecoverableIncompleteManagedConfiguration(t *testing.T) {
	dir := t.TempDir()
	if err := saveMetadata(filepath.Join(dir, "metadata.json"), Metadata{Version: "test", CreatedAt: time.Now(), UpdatedAt: time.Now()}); err != nil {
		t.Fatalf("save metadata: %v", err)
	}
	runner := &imageLoadRunner{}
	manager := &Manager{Dir: dir, Runner: runner, Stdout: io.Discard, Stderr: io.Discard}

	_, err := manager.ImageLoad(t.Context(), "demo:dev", ImageLoadOptions{})
	if err == nil || !strings.Contains(err.Error(), "run `axern local up` to rebuild it") {
		t.Fatalf("ImageLoad() error = %v", err)
	}
}

func TestExecRunnerPipeConnectsProducerAndConsumer(t *testing.T) {
	var output strings.Builder
	err := (ExecRunner{}).Pipe(t.Context(), &output, io.Discard,
		"/bin/sh", []string{"-c", "printf immutable-generation"},
		"/bin/sh", []string{"-c", "cat"},
	)
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	if output.String() != "immutable-generation" {
		t.Fatalf("Pipe() output = %q", output.String())
	}
}
