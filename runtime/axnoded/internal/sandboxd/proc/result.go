package proc

import (
	"os"
	"os/exec"
	"syscall"
)

type Result struct {
	ExitCode int
	Signal   os.Signal
	Err      error
}

func WaitStatusFromError(err error) (int, os.Signal) {
	if err == nil {
		return 0, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			return 128 + int(status.Signal()), status.Signal()
		}
		return exitErr.ExitCode(), nil
	}
	return RuntimeStartExitCode, nil
}

func ResultFromError(err error) Result {
	code, sig := WaitStatusFromError(err)
	result := Result{ExitCode: code, Signal: sig}
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			result.Err = err
		}
	}
	return result
}
