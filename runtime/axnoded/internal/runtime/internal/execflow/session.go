package execflow

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type SessionState struct {
	chunks  chan contract.Chunk
	waitCh  chan struct{}
	exit    contract.Exit
	waitErr error
	closeFn func() error
}

func NewSessionState() *SessionState {
	return &SessionState{
		chunks: make(chan contract.Chunk, 8),
		waitCh: make(chan struct{}),
	}
}

func (s *SessionState) SetCloseFunc(closeFn func() error) {
	s.closeFn = closeFn
}

func (s *SessionState) EmitStdout(data []byte) {
	if len(data) == 0 {
		return
	}
	buf := append([]byte(nil), data...)
	s.chunks <- contract.Chunk{Stdout: buf}
}

func (s *SessionState) EmitStderr(data []byte) {
	if len(data) == 0 {
		return
	}
	buf := append([]byte(nil), data...)
	s.chunks <- contract.Chunk{Stderr: buf}
}

func (s *SessionState) Recv() (contract.Chunk, error) {
	chunk, ok := <-s.chunks
	if !ok {
		return contract.Chunk{}, io.EOF
	}
	return chunk, nil
}

func (s *SessionState) Wait() (contract.Exit, error) {
	<-s.waitCh
	return s.exit, s.waitErr
}

func (s *SessionState) FinishWait(exit contract.Exit, err error) {
	s.exit = exit
	s.waitErr = err
	close(s.waitCh)
}

func (s *SessionState) FinishOutput() {
	close(s.chunks)
}

func (s *SessionState) Close() error {
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}

func (s *SessionState) Signal(string) error {
	return s.Close()
}

func ExitCodeFromError(err error) (int32, error) {
	if err == nil {
		return 0, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 0, err
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return int32(status.ExitStatus()), nil
		}
		return int32(exitErr.ExitCode()), nil
	}
	return 0, err
}

func EmitConsoleOutput(console *os.File, emit func([]byte)) {
	if console == nil {
		return
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := console.Read(buf)
		if n > 0 {
			emit(buf[:n])
		}
		if err == nil {
			continue
		}
		if errors.Is(err, io.EOF) {
			return
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && errors.Is(pathErr.Err, syscall.EIO) {
			return
		}
		return
	}
}
