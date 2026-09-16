//go:build windows

package proc

import (
	"syscall"
)

func SysProcAttr() *syscall.SysProcAttr {
	return nil
}
