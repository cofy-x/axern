//go:build !windows

package process

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func startTerminalProcess(cmd *exec.Cmd, size *pty.Winsize, user processUser, hasUser bool) (*os.File, error) {
	ptm, pts, err := pty.Open()
	if err != nil {
		return nil, err
	}
	started := false
	defer func() {
		_ = pts.Close()
		if !started {
			_ = ptm.Close()
		}
	}()

	if err := configureTerminalOutput(pts); err != nil {
		return nil, err
	}
	if size != nil {
		if err := pty.Setsize(ptm, size); err != nil {
			return nil, err
		}
	}
	cmd.Stdout = pts
	cmd.Stderr = pts
	cmd.Stdin = pts
	cmd.SysProcAttr = processSysProcAttr(user, hasUser, true)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	started = true
	return ptm, nil
}
