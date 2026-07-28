//go:build !aix && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package taskset

import "io/fs"

func fileHasMultipleLinks(fs.FileInfo) bool { return false }
