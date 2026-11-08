//go:build linux

package serverruntime

import (
	"os"
	"syscall"
)

func rootOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == 0
}
