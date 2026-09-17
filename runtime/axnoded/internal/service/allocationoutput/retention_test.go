package allocationoutput

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSealPublishesDeclaredOutputAtomically(t *testing.T) {
	root := t.TempDir()
	expiry := time.Now().Add(time.Minute).UTC()
	content := []byte("candidate bundle")
	digest := sha256.Sum256(content)
	r := NewRetention(root)
	captures := 0
	err := r.Seal(context.Background(), "allocation-one", expiry, Sources{Terminal: true}, func(_ context.Context, objects string) ([]Entry, error) {
		captures++
		object, err := CreateObject(objects, "object-one")
		if err != nil {
			return nil, err
		}
		if _, err := object.Write(content); err != nil {
			return nil, err
		}
		if err := object.Sync(); err != nil {
			return nil, err
		}
		if err := object.Close(); err != nil {
			return nil, err
		}
		return []Entry{{OutputID: "output-one", Path: "/output", Object: "object-one", SHA256: hex.EncodeToString(digest[:]), Format: "file", Status: "available", SizeBytes: int64(len(content)), SealedAt: time.Now().UTC()}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := r.Manifest("allocation-one", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 1 || manifest.Entries[0].SHA256 != hex.EncodeToString(digest[:]) {
		t.Fatalf("manifest = %+v", manifest)
	}
	data, next, eof, err := r.ReadSealedOutput("allocation-one", "output-one", 0, 4, time.Now())
	if err != nil || string(data) != "cand" || next != 4 || eof {
		t.Fatalf("first read = %q, %d, %t, %v", data, next, eof, err)
	}
	data, next, eof, err = r.ReadSealedOutput("allocation-one", "output-one", next, 64, time.Now())
	if err != nil || string(data) != "idate bundle" || next != int64(len(content)) || !eof {
		t.Fatalf("second read = %q, %d, %t, %v", data, next, eof, err)
	}
	if err := r.Seal(context.Background(), "allocation-one", expiry, Sources{}, func(context.Context, string) ([]Entry, error) {
		captures++
		return nil, errors.New("must not recapture")
	}); err != nil {
		t.Fatal(err)
	}
	if captures != 1 {
		t.Fatalf("captures = %d, want 1", captures)
	}
	objectPath := filepath.Join(root, "allocation-output", "allocation-one", "objects", "object-one")
	if err := os.WriteFile(objectPath, []byte("tampered! bundle"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := r.Seal(context.Background(), "allocation-one", expiry, Sources{}, nil); err == nil {
		t.Fatal("same-size sealed output corruption accepted")
	}
	if err := os.WriteFile(objectPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(objectPath); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.ReadSealedOutput("allocation-one", "output-one", 0, 64, time.Now()); !errors.Is(err, ErrSealedStateUnavailable) {
		t.Fatalf("missing sealed bytes = %v, want unavailable", err)
	}
	if err := r.Seal(context.Background(), "allocation-one", expiry, Sources{}, nil); err == nil {
		t.Fatal("incomplete sealed output accepted")
	}
}

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
