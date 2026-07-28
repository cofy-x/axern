package agentbundle

import "testing"

func TestLayout(t *testing.T) {
	target := MountTarget("Claude Code")
	if target != "/opt/axern/agents/claude-code" {
		t.Fatalf("MountTarget() = %q", target)
	}
	if got := BinDir(target); got != target+"/bin" {
		t.Fatalf("BinDir() = %q", got)
	}
	if got := MountedBinary(target, "/bin/claude"); got != target+"/bin/claude" {
		t.Fatalf("MountedBinary() = %q", got)
	}
}

func TestRejectsUnsafePaths(t *testing.T) {
	for _, target := range []string{"/", "/opt/axern/agents", "/opt/axern/agents/../codex", "/opt/axern/agents/codex/bin"} {
		if ValidMountTarget(target) {
			t.Errorf("ValidMountTarget(%q) = true", target)
		}
	}
	for _, binary := range []string{"", "bin/codex", "/", "/bin/../codex", "/bin/codex\x00"} {
		if ValidBinaryPath(binary) {
			t.Errorf("ValidBinaryPath(%q) = true", binary)
		}
	}
}
