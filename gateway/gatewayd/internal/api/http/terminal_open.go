package httpapi

import (
	"net/http"
	"path/filepath"
	"strings"

	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
)

func terminalOpenOptionsFromRequest(r *http.Request) term.OpenOptions {
	opts := term.OpenOptions{Env: map[string]string{"TERM": "xterm-256color"}, TTY: true}
	shell := strings.TrimSpace(r.URL.Query().Get("shell"))
	if shell == "" || !isSupportedTerminalShell(shell) {
		return opts
	}
	opts.Argv = []string{shell}
	return opts
}

func isSupportedTerminalShell(shell string) bool {
	switch filepath.Base(shell) {
	case "sh", "bash", "ash", "dash", "zsh":
		return true
	default:
		return false
	}
}
