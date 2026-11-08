//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

func processSysProcAttr(_ processUser, _ bool, _ bool) *syscall.SysProcAttr {
	return nil
}

func startProcessGroupWithUser(cmd *exec.Cmd, user processUser, hasUser bool) error {
	cleanup, err := prepareProcessUser(cmd, user, hasUser)
	if err != nil {
		return err
	}
	defer cleanup()
	return cmd.Start()
}
