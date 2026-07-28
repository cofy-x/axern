//go:build !linux

package process

import "os"

func configureTerminalOutput(_ *os.File) error {
	return nil
}
