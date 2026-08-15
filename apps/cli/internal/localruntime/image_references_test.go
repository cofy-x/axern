package localruntime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalImageReferenceMovesMutableAliasesAtomically(t *testing.T) {
	dir := t.TempDir()
	const (
		source    = "demo:dev"
		canonical = "index.docker.io/library/demo:dev"
		digestA   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		digestB   = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	immutableA := "index.docker.io/library/demo@" + digestA
	immutableB := "index.docker.io/library/demo@" + digestB
	if err := saveLocalImageReference(dir, source, canonical, immutableA, digestA); err != nil {
		t.Fatal(err)
	}
	if err := saveLocalImageReference(dir, source, canonical, immutableB, digestB); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{source, canonical} {
		got, pinned, err := ResolveLocalImageReference(dir, alias)
		if err != nil {
			t.Fatalf("ResolveLocalImageReference(%q) error = %v", alias, err)
		}
		if !pinned || got != immutableB {
			t.Fatalf("ResolveLocalImageReference(%q) = (%q, %t), want (%q, true)", alias, got, pinned, immutableB)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "image-references.json")); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("image reference mode = %o, want 600", info.Mode().Perm())
	}
}

func TestResolveLocalImageReferenceFailsClosedOnCorruptIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "image-references.json"), []byte(`{"version":1,"references":{"demo:dev":{"immutable_ref":"invalid"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveLocalImageReference(dir, "demo:dev"); err == nil {
		t.Fatal("ResolveLocalImageReference() error = nil for corrupt index")
	}
}
