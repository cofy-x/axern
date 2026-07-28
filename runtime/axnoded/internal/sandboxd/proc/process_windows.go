//go:build windows

package proc

import (
	"os"
	"syscall"
)

func SysProcAttr() *syscall.SysProcAttr {
	return nil
}

func SignalProcessGroup(pid int, signal os.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

func KillProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
