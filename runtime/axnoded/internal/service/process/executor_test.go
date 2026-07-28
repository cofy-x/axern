package process

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type execStreamStub struct {
	ctx       context.Context
	requests  []*runtime.ExecStreamRequest
	sent      []*runtime.ExecStreamResponse
	recvIndex int
}

func (s *execStreamStub) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *execStreamStub) Recv() (*runtime.ExecStreamRequest, error) {
	if s.recvIndex >= len(s.requests) {
		return nil, io.EOF
	}
	req := s.requests[s.recvIndex]
	s.recvIndex++
	return req, nil
}

func (s *execStreamStub) Send(resp *runtime.ExecStreamResponse) error {
	s.sent = append(s.sent, resp)
	return nil
}

type processStreamStub struct {
	ctx       context.Context
	requests  []*runtime.ProcessRequest
	sent      []*runtime.ProcessResponse
	recvIndex int
}

func (s *processStreamStub) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

func (s *processStreamStub) Recv() (*runtime.ProcessRequest, error) {
	if s.recvIndex >= len(s.requests) {
		return nil, io.EOF
	}
	req := s.requests[s.recvIndex]
	s.recvIndex++
	return req, nil
}

func (s *processStreamStub) Send(resp *runtime.ProcessResponse) error {
	s.sent = append(s.sent, resp)
	return nil
}

type sessionStub struct {
	chunks                []contract.Chunk
	exit                  contract.Exit
	waitErr               error
	writes                [][]byte
	resizeCalls           [][2]uint32
	signals               []string
	stdinClosed           bool
	blockOutputUntilStdin bool
	blockOutputCh         <-chan struct{}
	blockWaitCh           <-chan struct{}
	stdinClosedCh         chan struct{}
	closeOnce             sync.Once
}

func newSessionStub() *sessionStub {
	return &sessionStub{stdinClosedCh: make(chan struct{})}
}

func (s *sessionStub) Write(data []byte) error {
	s.writes = append(s.writes, append([]byte(nil), data...))
	return nil
}

func (s *sessionStub) CloseStdin() error {
	s.stdinClosed = true
	s.closeOnce.Do(func() { close(s.stdinClosedCh) })
	return nil
}

func (s *sessionStub) Resize(cols, rows uint32) error {
	s.resizeCalls = append(s.resizeCalls, [2]uint32{cols, rows})
	return nil
}

func (s *sessionStub) Signal(signal string) error {
	s.signals = append(s.signals, signal)
	return nil
}

func (s *sessionStub) Recv() (contract.Chunk, error) {
	if s.blockOutputCh != nil {
		<-s.blockOutputCh
		s.blockOutputCh = nil
	}
	if s.blockOutputUntilStdin {
		<-s.stdinClosedCh
		s.blockOutputUntilStdin = false
	}
	if len(s.chunks) == 0 {
		return contract.Chunk{}, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func (s *sessionStub) Wait() (contract.Exit, error) {
	if s.blockWaitCh != nil {
		<-s.blockWaitCh
		s.blockWaitCh = nil
	}
	return s.exit, s.waitErr
}

func (s *sessionStub) Close() error {
	return nil
}

func TestRunExecStreamForwardsInputOutputAndExit(t *testing.T) {
	session := newSessionStub()
	session.blockOutputUntilStdin = true
	session.chunks = []contract.Chunk{
		{Stdout: []byte("stdout")},
		{Stderr: []byte("stderr")},
	}
	session.exit = contract.Exit{Status: 7}
	stream := &execStreamStub{requests: []*runtime.ExecStreamRequest{
		{Payload: &runtime.ExecStreamRequest_Stdin{Stdin: []byte("payload")}},
		{Payload: &runtime.ExecStreamRequest_Resize{Resize: &runtime.TerminalResize{Cols: 100, Rows: 30}}},
		{Payload: &runtime.ExecStreamRequest_CloseStdin{CloseStdin: true}},
	}}

	result, err := RunExecStream(context.Background(), stream, session)
	require.NoError(t, err)
	assert.Equal(t, StreamResultExit, result)
	assert.Equal(t, [][]byte{[]byte("payload")}, session.writes)
	assert.Equal(t, [][2]uint32{{100, 30}}, session.resizeCalls)
	assert.True(t, session.stdinClosed)
	require.Len(t, stream.sent, 3)
	assert.Equal(t, []byte("stdout"), stream.sent[0].GetStdout())
	assert.Equal(t, []byte("stderr"), stream.sent[1].GetStderr())
	assert.Equal(t, int32(7), stream.sent[2].GetExit().GetExitCode())
}

func TestRunProcessStreamForwardsSignalOutputAndExit(t *testing.T) {
	session := newSessionStub()
	session.blockOutputUntilStdin = true
	session.chunks = []contract.Chunk{{Stdout: []byte("ok")}}
	session.exit = contract.Exit{Status: 0}
	stream := &processStreamStub{requests: []*runtime.ProcessRequest{
		{Payload: &runtime.ProcessRequest_Signal{Signal: &runtime.ProcessSignal{Signal: "TERM"}}},
		{Payload: &runtime.ProcessRequest_CloseStdin{CloseStdin: true}},
	}}

	result, err := RunProcessStream(context.Background(), stream, session)
	require.NoError(t, err)
	assert.Equal(t, StreamResultExit, result)
	assert.Equal(t, []string{"TERM"}, session.signals)
	require.Len(t, stream.sent, 2)
	assert.Equal(t, []byte("ok"), stream.sent[0].GetStdout())
	assert.Equal(t, int32(0), stream.sent[1].GetExit().GetExitCode())
}

func TestRunProcessStreamReturnsTimeoutResult(t *testing.T) {
	session := newSessionStub()
	unblockOutput := make(chan struct{})
	defer close(unblockOutput)
	session.blockOutputCh = unblockOutput
	stream := &processStreamStub{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	result, err := RunProcessStream(ctx, stream, session)
	require.Error(t, err)
	assert.Equal(t, StreamResultTimeout, result)
}

func TestProcessInputStopsMutatingSessionAfterStreamReturns(t *testing.T) {
	session := newSessionStub()
	input := newSessionInputGate(session)
	input.stop()
	result := make(chan error, 1)
	stream := &processStreamStub{requests: []*runtime.ProcessRequest{
		{Payload: &runtime.ProcessRequest_Stdin{Stdin: []byte("late")}},
	}}

	pumpProcessInput(stream, input, result)

	require.NoError(t, <-result)
	assert.Empty(t, session.writes)
}

func TestRunExecStreamTimesOutWaitingForExit(t *testing.T) {
	session := newSessionStub()
	unblockWait := make(chan struct{})
	defer close(unblockWait)
	session.blockWaitCh = unblockWait
	stream := &execStreamStub{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	result, err := RunExecStream(ctx, stream, session)
	require.Error(t, err)
	assert.Equal(t, StreamResultTimeout, result)
}
