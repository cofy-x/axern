package allocationoutput

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRetainedOutputSurvivesCleanupAndRestart(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "live")
	data := make([]byte, ChunkBytes*3+17)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(live, data, 0600); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Minute)
	r := NewRetention(root)
	if err := r.Preserve("allocation-one", expiry, Sources{Stdout: live, Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := r.Preserve("allocation-one", expiry, Sources{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(live); err != nil {
		t.Fatal(err)
	}
	reopened := NewRetention(root)
	reader := NewWithSources(func(_ context.Context, id string) (Sources, error) { return reopened.Sources(id, time.Now()) })
	cursor, total := "", 0
	for {
		chunks, complete, err := reader.Read(context.Background(), "allocation-one", cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range chunks {
			total += len(c.Data)
			cursor = c.Cursor
		}
		if complete {
			break
		}
		if len(chunks) == 0 {
			t.Fatal("stalled retained output")
		}
	}
	if total != len(data) {
		t.Fatalf("bytes=%d, want %d", total, len(data))
	}
	if _, err := reopened.Sources("allocation-one", expiry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expiry: %v", err)
	}
	if err := reopened.Sweep(expiry, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "allocation-output", "allocation-one")); !os.IsNotExist(err) {
		t.Fatalf("expired output remains: %v", err)
	}
}

func TestRetentionRejectsIdentityAndExpiryConflicts(t *testing.T) {
	r := NewRetention(t.TempDir())
	expiry := time.Now().Add(time.Minute)
	if err := r.Preserve("../escape", expiry, Sources{}); err == nil {
		t.Fatal("path escape accepted")
	}
	if err := r.Preserve("allocation-one", expiry, Sources{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Preserve("allocation-one", expiry.Add(time.Minute), Sources{}); err == nil {
		t.Fatal("expiry extension accepted")
	}
	path, _ := r.path("allocation-one")
	if err := os.Remove(filepath.Join(path, "stdout")); err != nil {
		t.Fatal(err)
	}
	if err := r.Preserve("allocation-one", expiry, Sources{}); err == nil {
		t.Fatal("incomplete snapshot accepted")
	}
}

func TestTruncatedOutputDrainsReadablePrefix(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxOutputBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	reader := NewWithSources(func(context.Context, string) (Sources, error) { return Sources{Stdout: path, Terminal: true}, nil })
	cursor, total := "", 0
	for {
		chunks, complete, err := reader.Read(context.Background(), "one", cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range chunks {
			total += len(c.Data)
			cursor = c.Cursor
		}
		if complete {
			if len(chunks) == 0 || !chunks[len(chunks)-1].Truncated {
				t.Fatal("missing truncation signal")
			}
			break
		}
	}
	if total != MaxOutputBytes {
		t.Fatalf("received %d bytes, want %d", total, MaxOutputBytes)
	}
}
