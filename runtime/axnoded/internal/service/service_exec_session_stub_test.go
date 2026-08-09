package service

import (
	"io"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type execSessionStub struct {
	mu            sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes = append(s.writes, append([]byte(nil), data...))
	return s.writeErr
}

func (s *execSessionStub) CloseStdin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stdinClosed = true
	return s.closeStdinErr
}

func (s *execSessionStub) Resize(cols, rows uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resizeCalls = append(s.resizeCalls, [2]uint32{cols, rows})
	return nil
}

func (s *execSessionStub) Signal(signal string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *execSessionStub) writesSnapshot() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([][]byte, len(s.writes))
	for i, write := range s.writes {
		result[i] = append([]byte(nil), write...)
	}
	return result
}

func (s *execSessionStub) resizeCallsSnapshot() [][2]uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]uint32(nil), s.resizeCalls...)
}

func (s *execSessionStub) signalsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.signals...)
}

func (s *execSessionStub) isStdinClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stdinClosed
}

func (s *execSessionStub) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
