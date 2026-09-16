package localstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFilePreservesCommittedTargetWhenRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "record.json")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("rename failed")
	err := atomicWriteFileWithRename(path, []byte("new\n"), 0o600, func(string, string) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("atomicWriteFileWithRename() error = %v, want %v", err, wantErr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\n" {
		t.Fatalf("target = %q, want old committed content", data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "record.json" {
		t.Fatalf("directory entries = %v, want only committed target", entries)
	}
}

func TestWriteJSONUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record.json")
	if err := writeJSON(path, map[string]string{"status": "ok"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
}

func TestWriteJSONExclusiveNeverOverwritesImmutablePlan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := writeJSONExclusive(path, map[string]string{"version": "first"}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONExclusive(path, map[string]string{"version": "second"}); err == nil {
		t.Fatal("second write unexpectedly replaced immutable plan")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "{\n  \"version\": \"first\"\n}\n" {
		t.Fatalf("plan = %s", data)
	}
}
