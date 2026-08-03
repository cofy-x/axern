package allocationoutput

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	runtimev1 "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

type fakeLister struct{ container *runtimev1.ContainerStatus }

func (f fakeLister) List(context.Context, *runtimev1.ListContainersRequest) (*runtimev1.ListContainersResponse, error) {
	return &runtimev1.ListContainersResponse{Containers: []*runtimev1.ContainerStatus{f.container}}, nil
}

func TestReaderSeparatesStreamsAndResumesCursor(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout")
	stderr := filepath.Join(dir, "stderr")
	if err := os.WriteFile(stdout, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderr, []byte("warning"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := New(fakeLister{&runtimev1.ContainerStatus{ID: "a", State: runtimev1.ContainerState_CONTAINER_EXITED, Stdout: stdout, Stderr: stderr}})
	chunks, done, err := reader.Read(context.Background(), "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if !done || len(chunks) != 2 || chunks[0].Stream != "stdout" || string(chunks[0].Data) != "hello" || chunks[1].Stream != "stderr" || string(chunks[1].Data) != "warning" {
		t.Fatalf("unexpected chunks: %#v, done=%v", chunks, done)
	}
	if chunks[0].Terminal || !chunks[1].Terminal {
		t.Fatalf("terminal marker must appear only on the final chunk: %#v", chunks)
	}
	resumed, done, err := reader.Read(context.Background(), "a", chunks[len(chunks)-1].Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if !done || len(resumed) != 1 || !resumed[0].Terminal || len(resumed[0].Data) != 0 {
		t.Fatalf("unexpected resumed chunks: %#v, done=%v", resumed, done)
	}
}

func TestReaderEmitsRunningTruncationOnlyOnce(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout")
	if err := os.WriteFile(stdout, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(stdout, MaxOutputBytes); err != nil {
		t.Fatal(err)
	}
	reader := New(fakeLister{&runtimev1.ContainerStatus{ID: "a", State: runtimev1.ContainerState_CONTAINER_RUNNING, Stdout: stdout}})
	cursor := encodeCursor(MaxOutputBytes, 0, false)
	chunks, done, err := reader.Read(context.Background(), "a", cursor)
	if err != nil || done || len(chunks) != 1 || !chunks[0].Truncated || chunks[0].Terminal {
		t.Fatalf("unexpected first truncation result: %#v, done=%v, err=%v", chunks, done, err)
	}
	chunks, done, err = reader.Read(context.Background(), "a", chunks[0].Cursor)
	if err != nil || done || len(chunks) != 0 {
		t.Fatalf("truncation marker repeated: %#v, done=%v, err=%v", chunks, done, err)
	}
}

func TestReaderRejectsCursorBeyondAvailableOutput(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "stdout")
	if err := os.WriteFile(stdout, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := New(fakeLister{&runtimev1.ContainerStatus{ID: "a", State: runtimev1.ContainerState_CONTAINER_RUNNING, Stdout: stdout}})
	if _, _, err := reader.Read(context.Background(), "a", encodeCursor(6, 0, false)); err == nil {
		t.Fatal("cursor beyond available output succeeded")
	}
}

func TestDecodeCursorRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"bad", encodeCursor(-1, 0, false), encodeCursor(0, -1, false)} {
		if _, _, _, err := decodeCursor(value); err == nil {
			t.Fatalf("decodeCursor(%q) succeeded", value)
		}
	}
}
