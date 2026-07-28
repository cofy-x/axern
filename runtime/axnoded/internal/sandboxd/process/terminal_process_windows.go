//go:build windows

package process

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func startTerminalProcess(cmd *exec.Cmd, size *pty.Winsize, _ processUser, _ bool) (*os.File, error) {
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if size != nil {
		return pty.StartWithSize(cmd, size)
	}
	return pty.Start(cmd)
}
