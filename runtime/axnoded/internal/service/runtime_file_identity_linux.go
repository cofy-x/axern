//go:build linux

package service

import (
	"os"
	"syscall"
	"time"
)

// A write and restored mtime can share a coarse filesystem ctime tick. Do not
// retain a digest first observed inside that window, even if its stat is equal.
func runtimeFileMetadataCacheable(info os.FileInfo, now time.Time) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && now.Sub(time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)) >= time.Second
}

// runtimeFileMetadataEqual compares every kernel timestamp that can reveal an
// in-place runtime binary rewrite. Size and mtime alone are insufficient: a
// writer can preserve both, whereas ctime changes with content or metadata.
func runtimeFileMetadataEqual(left, right os.FileInfo) bool {
	leftStat, leftOK := left.Sys().(*syscall.Stat_t)
	rightStat, rightOK := right.Sys().(*syscall.Stat_t)
	if !leftOK || !rightOK {
		return false
	}
	return leftStat.Dev == rightStat.Dev &&
		leftStat.Ino == rightStat.Ino &&
		leftStat.Mode == rightStat.Mode &&
		leftStat.Size == rightStat.Size &&
		leftStat.Mtim == rightStat.Mtim &&
		leftStat.Ctim == rightStat.Ctim
}
