//go:build !windows

package proc

import (
	"fmt"
	"os"
	"syscall"
)

func SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func signalProcessGroup(pid int, signal os.Signal) error {
	if pid <= 0 {
		return fmt.Errorf("invalid process group: %d", pid)
	}
	sig, ok := signal.(syscall.Signal)
	if !ok {
		return fmt.Errorf("unsupported process signal: %v", signal)
	}
	return syscall.Kill(-pid, sig)
}
