package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestJSONStoreRoundTripAndPermissions(t *testing.T) {
	root := t.TempDir()
	store := NewJSONStore(root)
	m := newTestManager(t, store)
	want := prepare(t, m, "alloc-1", 1, "10.0.0.8", 1, dnsDeny("example.com"))

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].GetPolicyDigest() != want.GetPolicyDigest() {
		t.Fatalf("unexpected stored records: %#v", loaded)
	}
	info, err := os.Stat(filepath.Join(root, "prepared_policies.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestJSONStoreRejectsCorruptState(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "prepared_policies.json")
	if err := os.WriteFile(path, []byte(`{"not":"an array"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(NewJSONStore(root)); err == nil {
		t.Fatal("NewManager accepted corrupt persisted state")
	}
}
