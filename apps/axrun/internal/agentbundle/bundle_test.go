package agentbundle

import "testing"

func TestMountTargetSanitizesAgentName(t *testing.T) {
	for name, want := range map[string]string{
		"claude-code":      "/opt/axern/agents/claude-code",
		"Claude Code":      "/opt/axern/agents/claude-code",
		"custom/agent.v1":  "/opt/axern/agents/custom-agent-v1",
		".":                "/opt/axern/agents/agent",
		"..":               "/opt/axern/agents/agent",
		"../../etc/passwd": "/opt/axern/agents/etc-passwd",
	} {
		if got := MountTarget(name); got != want {
			t.Fatalf("MountTarget(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValidMountTargetAndBinDir(t *testing.T) {
	if !ValidMountTarget("/opt/axern/agents/codex") {
		t.Fatal("valid mount target rejected")
	}
	if !ValidBinDir("/opt/axern/agents/codex", "/opt/axern/agents/codex/bin") {
		t.Fatal("valid bin dir rejected")
	}
	for _, target := range []string{"/", "/opt/axern", "/opt/axern/agents", "/opt/axern/agents/a/b"} {
		if ValidMountTarget(target) {
			t.Fatalf("invalid mount target accepted: %q", target)
		}
	}
	if ValidBinDir("/opt/axern/agents/codex", "/opt/axern/agents/other/bin") {
		t.Fatal("invalid bin dir accepted")
	}
}
