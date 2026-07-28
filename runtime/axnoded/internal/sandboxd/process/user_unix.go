//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

func processSysProcAttr(user processUser, hasUser bool, terminal bool) *syscall.SysProcAttr {
	attr := &syscall.SysProcAttr{}
	if terminal {
		attr.Setsid = true
		attr.Setctty = true
	} else {
		attr.Setpgid = true
	}
	if hasUser {
		attr.Credential = &syscall.Credential{
			Uid:    user.uid,
			Gid:    user.gid,
			Groups: user.groups,
		}
	}
	return attr
}

func startProcessGroupWithUser(cmd *exec.Cmd, user processUser, hasUser bool) error {
	cmd.SysProcAttr = processSysProcAttr(user, hasUser, false)
	return cmd.Start()
}
