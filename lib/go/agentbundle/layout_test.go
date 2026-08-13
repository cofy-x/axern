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
	if got := ImageMountTarget(target); got != "/__claude_code" {
		t.Fatalf("ImageMountTarget() = %q", got)
	}
	if got := ImageMountTarget("/opt/axern/agents/codex"); got != "/opt/axern/agents/codex" {
		t.Fatalf("ImageMountTarget(codex) = %q", got)
	}
	claimed := ClaimedMountTargets(ClaudeCodeABIMountTarget)
	if len(claimed) != 2 || claimed[0] != ClaudeCodeABIMountTarget || claimed[1] != ClaudeCodeMountTarget {
		t.Fatalf("ClaimedMountTargets() = %#v", claimed)
	}
}

func TestRejectsUnsafePaths(t *testing.T) {
	for _, target := range []string{"/", "/__claude_code", "/__claude_code/bin", "/opt/axern/agents", "/opt/axern/agents/../codex", "/opt/axern/agents/codex/bin"} {
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
