package service

import (
	"io"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type execSessionStub struct {
	chunks        []contract.Chunk
	exit          contract.Exit
	err           error
	writes        [][]byte
	stdinClosed   bool
	resizeCalls   [][2]uint32
	signals       []string
	closed        bool
	writeErr      error
	closeStdinErr error
	recvEOFWait   <-chan struct{}
}

func (s *execSessionStub) Write(data []byte) error {
	s.writes = append(s.writes, append([]byte(nil), data...))
	return s.writeErr
}

func (s *execSessionStub) CloseStdin() error {
	s.stdinClosed = true
	return s.closeStdinErr
}

func (s *execSessionStub) Resize(cols, rows uint32) error {
	s.resizeCalls = append(s.resizeCalls, [2]uint32{cols, rows})
	return nil
}

func (s *execSessionStub) Signal(signal string) error {
	s.signals = append(s.signals, signal)
	return nil
}

func (s *execSessionStub) Recv() (contract.Chunk, error) {
	if len(s.chunks) == 0 {
		if s.recvEOFWait != nil {
			<-s.recvEOFWait
		}
		return contract.Chunk{}, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *execSessionStub) Wait() (contract.Exit, error) {
	return s.exit, s.err
}

func (s *execSessionStub) Close() error {
	s.closed = true
	return nil
}
