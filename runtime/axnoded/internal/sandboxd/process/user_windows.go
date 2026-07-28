//go:build windows

package process

import (
	"os/exec"
	"syscall"
)

func processSysProcAttr(_ processUser, _ bool, _ bool) *syscall.SysProcAttr {
	return nil
}

func startProcessGroupWithUser(cmd *exec.Cmd, _ processUser, _ bool) error {
	return cmd.Start()
}
