package sshapi

import (
	"path/filepath"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

type ptyRequest struct {
	Term     string
	Cols     uint32
	Rows     uint32
	WidthPx  uint32
	HeightPx uint32
	Modes    string
}

type ptySpec struct {
	Term string
	Cols uint32
	Rows uint32
}

type windowChangeRequest struct {
	Cols     uint32
	Rows     uint32
	WidthPx  uint32
	HeightPx uint32
}

type envRequest struct {
	Name  string
	Value string
}

const execUserEnvName = "AXERN_EXEC_USER"

func parseEnvRequest(payload []byte) (string, string, bool) {
	var req envRequest
	if err := gossh.Unmarshal(payload, &req); err != nil {
		return "", "", false
	}
	return strings.TrimSpace(req.Name), strings.TrimSpace(req.Value), true
}

func validContainerUser(user string) bool {
	user = strings.TrimSpace(user)
	if user == "" || len(user) > 128 {
		return false
	}
	userPart, groupPart, hasGroup := strings.Cut(user, ":")
	if userPart == "" || (hasGroup && groupPart == "") {
		return false
	}
	for _, part := range []string{userPart, groupPart} {
		if part == "" {
			continue
		}
		for _, r := range part {
			switch {
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case r >= '0' && r <= '9':
			case r == '_' || r == '-' || r == '.':
			default:
				return false
			}
		}
	}
	return true
}

func parsePTYRequest(payload []byte) (ptySpec, bool) {
	var req ptyRequest
	if err := gossh.Unmarshal(payload, &req); err != nil {
		return ptySpec{}, false
	}
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	return ptySpec{Term: strings.TrimSpace(req.Term), Cols: req.Cols, Rows: req.Rows}, true
}

func parseWindowChange(payload []byte) (uint32, uint32, bool) {
	var req windowChangeRequest
	if err := gossh.Unmarshal(payload, &req); err != nil || req.Cols == 0 || req.Rows == 0 {
		return 0, 0, false
	}
	return req.Cols, req.Rows, true
}

func isUnsupportedSessionRequest(requestType string) bool {
	switch requestType {
	case "subsystem", "auth-agent-req@openssh.com", "x11-req":
		return true
	default:
		return false
	}
}

type execRequest struct {
	Command string
}

func parseExecRequest(payload []byte) ([]string, bool) {
	var req execRequest
	if err := gossh.Unmarshal(payload, &req); err != nil {
		return nil, false
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		return nil, false
	}
	if argv, ok := parseShellCommand(command); ok {
		return argv, true
	}
	return []string{"/bin/sh", "-lc", command}, true
}

func parseShellCommand(command string) ([]string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 || len(fields) > 2 {
		return nil, false
	}
	if !isSupportedShell(fields[0]) {
		return nil, false
	}
	if len(fields) == 2 && fields[1] != "-l" && fields[1] != "--login" {
		return nil, false
	}
	return append([]string(nil), fields...), true
}

func isSupportedShell(shell string) bool {
	switch filepath.Base(shell) {
	case "sh", "bash", "ash", "dash", "zsh":
		return true
	default:
		return false
	}
}

const defaultTerminalName = "xterm-256color"

func portableTerminalName(term string) string {
	term = strings.TrimSpace(term)
	if term == "" {
		return defaultTerminalName
	}
	if len(term) > 64 {
		return defaultTerminalName
	}
	for _, r := range term {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '+':
		default:
			return defaultTerminalName
		}
	}
	switch term {
	case "ansi",
		"linux",
		"screen",
		"screen-256color",
		"tmux",
		"tmux-256color",
		"vt100",
		"vt220",
		"xterm",
		"xterm-color",
		"xterm-256color":
		return term
	default:
		return defaultTerminalName
	}
}
