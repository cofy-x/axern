package durablefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicallyReplacesContentAndMode(t *testing.T) {
	target := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new" {
		t.Fatalf("content = %q", content)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("mode = %o", got)
	}
}
