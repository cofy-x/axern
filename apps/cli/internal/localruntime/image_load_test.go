package localruntime

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

type imageLoadRunner struct {
	outputs         map[string][]byte
	sourceArgs      []string
	destinationArgs []string
}

func (r *imageLoadRunner) Run(context.Context, io.Writer, io.Writer, string, ...string) error {
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
	_, err := io.WriteString(stdout, `{"source_ref":"demo:dev","canonical_ref":"index.docker.io/library/demo:dev","immutable_ref":"index.docker.io/library/demo@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generation_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","archive_digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","platform":"linux/amd64","size_bytes":42,"reused":false}`)
	return err
}

func TestImageLoadStreamsImmutableImageIDIntoNode(t *testing.T) {
	dir := t.TempDir()
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
	if result.GenerationDigest == "" {
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
