//go:build linux

package main

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

func enableChildSubreaper() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

func waitForChildExit(pid int) (int, error) {
	var status syscall.WaitStatus
	for {
		waitedPID, err := syscall.Wait4(pid, &status, 0, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if waitedPID != pid {
			return 0, fmt.Errorf("waited for unexpected pid %d", waitedPID)
		}
		if status.Signaled() {
			return 128 + int(status.Signal()), nil
		}
		return status.ExitStatus(), nil
	}
}
