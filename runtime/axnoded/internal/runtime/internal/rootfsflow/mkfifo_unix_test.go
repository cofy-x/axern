//go:build unix

package rootfsflow

import "golang.org/x/sys/unix"

func syscallMkfifo(path string, mode uint32) error {
	return unix.Mkfifo(path, mode)
}
