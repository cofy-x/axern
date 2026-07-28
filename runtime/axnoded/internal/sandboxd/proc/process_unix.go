//go:build !windows

package proc

import (
	"os"
	"syscall"
)

func SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func SignalProcessGroup(pid int, signal os.Signal) error {
	sig, ok := signal.(syscall.Signal)
	if !ok {
		sig = syscall.SIGTERM
	}
	return syscall.Kill(-pid, sig)
}

func KillProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
