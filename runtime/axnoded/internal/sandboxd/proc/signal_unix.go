//go:build !windows

package proc

import (
	"os"
	"syscall"
)

func SignalByNumber(value int) (os.Signal, error) {
	return syscall.Signal(value), nil
}

func SignalByName(value string) (os.Signal, bool) {
	switch value {
	case "TERM":
		return syscall.SIGTERM, true
	case "INT":
		return syscall.SIGINT, true
	case "KILL":
		return syscall.SIGKILL, true
	case "QUIT":
		return syscall.SIGQUIT, true
	case "HUP":
		return syscall.SIGHUP, true
	default:
		return nil, false
	}
}
