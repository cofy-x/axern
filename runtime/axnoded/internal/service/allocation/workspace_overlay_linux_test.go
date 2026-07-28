//go:build linux

package allocation

import (
	"os"
	"testing"
)

func TestMakeWorkspaceRootWritableForArbitraryNumericAgentUser(t *testing.T) {
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := makeWorkspaceRootWritable(workspace); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o777 {
		t.Fatalf("workspace root mode = %#o, want 0777", got)
	}
}
