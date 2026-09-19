//go:build darwin || linux

package localruntime

import (
	"os"
	"syscall"
)

type allocatedFileIdentity struct {
	device uint64
	inode  uint64
}

func allocatedFileUsage(info os.FileInfo) (int64, allocatedFileIdentity, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), allocatedFileIdentity{}, false
	}
	return int64(stat.Blocks) * 512, allocatedFileIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, true
}
