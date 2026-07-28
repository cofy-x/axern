//go:build linux

package process

import (
	"os"

	"golang.org/x/sys/unix"
)

func configureTerminalOutput(tty *os.File) error {
	termios, err := unix.IoctlGetTermios(int(tty.Fd()), unix.TCGETS)
	if err != nil {
		return err
	}
	termios.Oflag |= unix.OPOST | unix.ONLCR
	return unix.IoctlSetTermios(int(tty.Fd()), unix.TCSETS, termios)
}
