package execflow

import (
	"errors"
	"os"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const MaxExecOutputBytes = 1 << 20

func ReadOutputFileLimited(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if len(data) <= MaxExecOutputBytes {
		return data, false, nil
	}
	return data[:MaxExecOutputBytes], true, nil
}

func ResizeConsole(console *os.File, cols, rows uint32) error {
	if console == nil {
		return nil
	}
	return pty.Setsize(console, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

func WriteConsole(console *os.File, data []byte, writeChunk func(*os.File, []byte) (int, error), waitWritable func(*os.File) error) error {
	if console == nil {
		return nil
	}
	for len(data) > 0 {
		n, err := writeChunk(console, data)
		if n > 0 {
			data = data[n:]
		}
		if err == nil {
			continue
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			if waitErr := waitWritable(console); waitErr != nil {
				return waitErr
			}
			continue
		}
		return err
	}
	return nil
}

func DefaultConsoleWriteChunk(console *os.File, data []byte) (int, error) {
	return console.Write(data)
}

func WaitConsoleWritable(console *os.File) error {
	for {
		_, err := unix.Poll([]unix.PollFd{{
			Fd:     int32(console.Fd()),
			Events: unix.POLLOUT,
		}}, -1)
		if err == nil {
			return nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		return err
	}
}
