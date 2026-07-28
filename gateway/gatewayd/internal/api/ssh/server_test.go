package sshapi

import (
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestParsePTYRequest(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Term     string
		Cols     uint32
		Rows     uint32
		WidthPx  uint32
		HeightPx uint32
		Modes    string
	}{Term: "xterm", Cols: 120, Rows: 40})

	pty, ok := parsePTYRequest(payload)
	if !ok || pty.Term != "xterm" || pty.Cols != 120 || pty.Rows != 40 {
		t.Fatalf("parsePTYRequest() = %#v ok=%v", pty, ok)
	}
}

func TestParsePTYRequestDefaultsZeroDimensions(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Term     string
		Cols     uint32
		Rows     uint32
		WidthPx  uint32
		HeightPx uint32
		Modes    string
	}{Term: "xterm", Cols: 0, Rows: 0})

	pty, ok := parsePTYRequest(payload)
	if !ok || pty.Cols != 80 || pty.Rows != 24 {
		t.Fatalf("parsePTYRequest() = %#v ok=%v", pty, ok)
	}
}

func TestParseWindowChange(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Cols     uint32
		Rows     uint32
		WidthPx  uint32
		HeightPx uint32
	}{Cols: 100, Rows: 30})

	cols, rows, ok := parseWindowChange(payload)
	if !ok || cols != 100 || rows != 30 {
		t.Fatalf("parseWindowChange() = cols=%d rows=%d ok=%v", cols, rows, ok)
	}
}

func TestParseWindowChangeRejectsZeroDimensions(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Cols     uint32
		Rows     uint32
		WidthPx  uint32
		HeightPx uint32
	}{Cols: 0, Rows: 30})

	if _, _, ok := parseWindowChange(payload); ok {
		t.Fatal("parseWindowChange() ok = true, want false")
	}
}

func TestParseEnvRequest(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Name  string
		Value string
	}{Name: "AXERN_EXEC_USER", Value: " axern "})

	name, value, ok := parseEnvRequest(payload)
	if !ok || name != "AXERN_EXEC_USER" || value != "axern" {
		t.Fatalf("parseEnvRequest() = name=%q value=%q ok=%v", name, value, ok)
	}
}

func TestValidContainerUser(t *testing.T) {
	t.Parallel()
	for _, user := range []string{"axern", "1000", "1000:1000", "axern:axern", "node.user-1"} {
		if !validContainerUser(user) {
			t.Fatalf("validContainerUser(%q) = false, want true", user)
		}
	}
	for _, user := range []string{"", ":group", "user:", "bad user", "a:b:c", "../root", "root;id"} {
		if validContainerUser(user) {
			t.Fatalf("validContainerUser(%q) = true, want false", user)
		}
	}
}

func TestUnsupportedSessionRequests(t *testing.T) {
	t.Parallel()
	for _, requestType := range []string{"subsystem", "auth-agent-req@openssh.com", "x11-req"} {
		if !isUnsupportedSessionRequest(requestType) {
			t.Fatalf("isUnsupportedSessionRequest(%q) = false, want true", requestType)
		}
	}
	if isUnsupportedSessionRequest("shell") {
		t.Fatal("isUnsupportedSessionRequest(shell) = true, want false")
	}
	if isUnsupportedSessionRequest("exec") {
		t.Fatal("isUnsupportedSessionRequest(exec) = true, want false")
	}
}

func TestParseExecRequestUsesSupportedShellDirectly(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Command string
	}{Command: "/bin/bash -l"})

	argv, ok := parseExecRequest(payload)
	if !ok || len(argv) != 2 || argv[0] != "/bin/bash" || argv[1] != "-l" {
		t.Fatalf("parseExecRequest() = %#v ok=%v", argv, ok)
	}
}

func TestParseExecRequestWrapsArbitraryCommand(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Command string
	}{Command: "claude -p 'hello world'"})

	argv, ok := parseExecRequest(payload)
	if !ok || len(argv) != 3 || argv[0] != "/bin/sh" || argv[1] != "-lc" || argv[2] != "claude -p 'hello world'" {
		t.Fatalf("parseExecRequest() = %#v ok=%v", argv, ok)
	}
}

func TestParseExecRequestRejectsEmptyCommand(t *testing.T) {
	t.Parallel()
	payload := gossh.Marshal(struct {
		Command string
	}{Command: ""})
	if argv, ok := parseExecRequest(payload); ok {
		t.Fatalf("parseExecRequest(empty) = %#v ok=true, want false", argv)
	}
}

func TestPortableTerminalName(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"":                   defaultTerminalName,
		"xterm-256color":     "xterm-256color",
		"screen-256color":    "screen-256color",
		"tmux-256color":      "tmux-256color",
		"xterm-ghostty":      defaultTerminalName,
		"xterm-kitty":        defaultTerminalName,
		"bad term":           defaultTerminalName,
		"xterm;touch /tmp/x": defaultTerminalName,
	}
	for in, want := range tests {
		if got := portableTerminalName(in); got != want {
			t.Fatalf("portableTerminalName(%q) = %q, want %q", in, got, want)
		}
	}
}
