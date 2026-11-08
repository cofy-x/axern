//go:build windows

package process

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func startTerminalProcess(cmd *exec.Cmd, size *pty.Winsize, user processUser, hasUser bool) (*os.File, error) {
	cleanup, err := prepareProcessUser(cmd, user, hasUser)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if size != nil {
		return pty.StartWithSize(cmd, size)
	}
	return pty.Start(cmd)
}
